# k8sqlite

This program connects to Kubernetes API server and fetches configured
API resources into in-memory sqlite3 database.
It provides HTTP interface to query and backup this database.

## Example

Run using current `kubectl` context:
```sh
go run .
```

or deploy into the cluster:
```sh
kubectl apply -f deploy/

kubectl port-forward deploy/k8sqlite 5351
```

and query:
```sh
curl localhost:5351/query --data-urlencode "q=select count(*) as total, kind from resources group by kind order by total desc;"
```

```json
{"total":46,"kind":"ServiceAccount"}
{"total":12,"kind":"Pod"}
{"total":8,"kind":"ReplicaSet"}
{"total":5,"kind":"Namespace"}
{"total":3,"kind":"Deployment"}
{"total":2,"kind":"ControllerRevision"}
{"total":2,"kind":"DaemonSet"}
{"total":2,"kind":"Service"}
{"total":1,"kind":"Node"}
```
