package main

import (
	"context"
	"database/sql"
	_ "embed"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	_ "modernc.org/sqlite"
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

	fResources := flag.String("resources", "deployments.v1.apps,pods.v1.", "comma-separated list of resources")
	flag.Parse()

	ctx := context.Background()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db := must(sql.Open("sqlite", "file::memory:?cache=shared"))
	defer db.Close()

	must(0, db.PingContext(ctx))
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

	factory := dynamicinformer.NewDynamicSharedInformerFactory(client, 1*time.Second)
	defer factory.Shutdown()

	toJson := func(obj any) []byte {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			panic(fmt.Sprintf("Unexpected type: %T %v", obj, obj))
		}
		return must(u.MarshalJSON())
	}

	for resource := range strings.SplitSeq(*fResources, ",") {
		slog.Info("Watching resource", slog.String("resource", resource))

		gvr1, _ := schema.ParseResourceArg(resource)
		if gvr1 == nil {
			panic("Unsupported resource: " + resource)
		}
		//gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
		gvr := *gvr1
		genericInformer := factory.ForResource(gvr)

		gvrlog := slog.With(slog.Any("gvr", gvr))

		handle := must(genericInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				r := must(upsertStmt.ExecContext(ctx, toJson(obj)))
				gvrlog.Debug("add", slog.Int64("id", must(r.LastInsertId())))
			},
			UpdateFunc: func(_, obj any) {
				r := must(upsertStmt.ExecContext(ctx, toJson(obj)))
				gvrlog.Debug("update", slog.Int64("id", must(r.LastInsertId())))
			},
			DeleteFunc: func(obj any) {
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					panic(fmt.Sprintf("Unexpected type: %T %v", obj, obj))
				}
				r := must(deleteStmt.ExecContext(ctx, u.GetKind(), u.GetNamespace(), u.GetName()))
				gvrlog.Debug("delete", slog.Int64("id", must(r.LastInsertId())))
			},
		}))
		defer genericInformer.Informer().RemoveEventHandler(handle)
	}

	factory.Start(ctx.Done())

	synced := factory.WaitForCacheSync(ctx.Done())
	for v, ok := range synced {
		slog.Info("cache synced", slog.Any("type", v), slog.Bool("ok", ok))
	}

	<-ctx.Done()
}
