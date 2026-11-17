package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json/jsontext"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"modernc.org/sqlite"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err.Error())
	}
	return v
}

//go:embed schema.sql
var schemaSql string

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	flags := struct {
		resources string
		output    string
		watch     bool
		resync    time.Duration
	}{}
	flag.StringVar(&flags.resources, "resources", "deployments.v1.apps,pods.v1.", "comma-separated list of resources")
	flag.StringVar(&flags.output, "output", "", "output database file")
	flag.BoolVar(&flags.watch, "watch", false, "watch updates")
	flag.DurationVar(&flags.resync, "resync", 0, "informer resync interval")
	flag.Parse()

	ctx, cancelCause := context.WithCancelCause(context.Background())
	defer func() { slog.Info("Exiting", slog.Any("cause", context.Cause(ctx))) }()

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db := must(sql.Open("sqlite", "file::memory:?cache=shared"))
	defer db.Close()

	must("ping", db.PingContext(ctx))
	must(db.ExecContext(ctx, schemaSql))

	upsertStmt := must(db.PrepareContext(ctx, `
		INSERT INTO resources(object) VALUES(?)
		ON CONFLICT(kind, namespace, name) DO UPDATE SET object=object;
	`))
	defer upsertStmt.Close()

	deleteStmt := must(db.PrepareContext(ctx, `
		DELETE FROM resources
		WHERE kind = ?
		AND namespace = ?
		AND name = ?;
	`))
	defer upsertStmt.Close()

	config := must(clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile))
	client := must(dynamic.NewForConfig(config))

	factory := dynamicinformer.NewDynamicSharedInformerFactory(client, flags.resync)
	defer factory.Shutdown()

	toUnstructured := func(obj any) *unstructured.Unstructured {
		if u, ok := obj.(*unstructured.Unstructured); ok {
			return u
		}
		panic(fmt.Sprintf("Unexpected type: %T %v", obj, obj))
	}

	for resource := range strings.SplitSeq(flags.resources, ",") {
		slog.Info("Watching resource", slog.String("resource", resource))

		gvrp, _ := schema.ParseResourceArg(resource)
		if gvrp == nil {
			panic("Unsupported resource: " + resource)
		}
		gvr := *gvrp
		genericInformer := factory.ForResource(gvr)

		gvrlog := slog.With(slog.Any("gvr", gvr))

		handle := must(genericInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				u := toUnstructured(obj)
				must(upsertStmt.ExecContext(ctx, must(u.MarshalJSON())))
				gvrlog.With().Debug("Add",
					slog.String("ns", u.GetNamespace()),
					slog.String("name", u.GetName()))
			},
			UpdateFunc: func(_, obj any) {
				u := toUnstructured(obj)
				must(upsertStmt.ExecContext(ctx, must(u.MarshalJSON())))
				gvrlog.With().Debug("Update",
					slog.String("ns", u.GetNamespace()),
					slog.String("name", u.GetName()))
			},
			DeleteFunc: func(obj any) {
				u := toUnstructured(obj)
				must(deleteStmt.ExecContext(ctx, u.GetKind(), u.GetNamespace(), u.GetName()))
				gvrlog.With().Debug("Delete",
					slog.String("ns", u.GetNamespace()),
					slog.String("name", u.GetName()))
			},
		}))
		defer genericInformer.Informer().RemoveEventHandler(handle)
	}

	factory.Start(ctx.Done())

	synced := factory.WaitForCacheSync(ctx.Done())
	for v, ok := range synced {
		slog.Info("Cache synced", slog.Any("type", v), slog.Bool("ok", ok))
	}

	if flags.output != "" {
		slog.Info("Writing database")
		must("backup", backup(ctx, db, flags.output))
	} else {
		slog.Info("Output is not specified, skipping")
	}

	if !flags.watch {
		cancelCause(fmt.Errorf("not watching"))
		return
	}

	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.FormValue("q")
		if q == "" {
			http.Error(w, "q required", http.StatusBadRequest)
			return
		}

		rows, err := db.QueryContext(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		writeJson(w, rows)
	})

	if flags.output != "" {
		http.HandleFunc("/backup", func(w http.ResponseWriter, r *http.Request) {
			must("backup", backup(ctx, db, flags.output))

			w.WriteHeader(http.StatusNoContent)
		})
	}

	go func() { cancelCause(http.ListenAndServe(":8080", nil)) }()

	<-ctx.Done()
}

func backup(ctx context.Context, db *sql.DB, path string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) error {
		backup, err := driverConn.(interface {
			NewBackup(dstUri string) (*sqlite.Backup, error)
		}).NewBackup(path)
		if err != nil {
			return err
		}
		more, err := backup.Step(-1)
		if err != nil || more {
			return fmt.Errorf("failed to step: %t, %w", more, err)
		}
		return backup.Finish()
	})
}

func writeJson(w http.ResponseWriter, rows *sql.Rows) {
	w.Header().Set("Content-Type", "application/jsonl")
	enc := jsontext.NewEncoder(w)
	columns := must(rows.ColumnTypes())
	for rows.Next() {
		rdata := make([]any, len(columns))
		for i, typ := range columns {
			rdata[i] = reflect.New(typ.ScanType()).Interface()
		}
		must("scan", rows.Scan(rdata...))
		must("begin", enc.WriteToken(jsontext.BeginObject))
		for i := range rdata {
			must("name", enc.WriteToken(jsontext.String(columns[i].Name())))

			val := reflect.ValueOf(rdata[i]).Elem().Interface()
			switch vv := val.(type) {
			case []byte:
				must("val", enc.WriteToken(jsontext.String(string(vv))))
			case string:
				must("val", enc.WriteToken(jsontext.String(vv)))
			case int64:
				must("val", enc.WriteToken(jsontext.Int(vv)))
			default:
				slog.Error("Unsupported type", slog.Any("type", fmt.Sprintf("%T", val)))
				must("val", enc.WriteToken(jsontext.String(fmt.Sprintf("%v", val))))
			}
		}
		must("end", enc.WriteToken(jsontext.EndObject))
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
