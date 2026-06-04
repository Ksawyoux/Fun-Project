module archgraph/pipeline

go 1.22

require (
	archgraph/nif v0.0.0
	archgraph/storage v0.0.0
	github.com/mattn/go-sqlite3 v1.14.22
)

replace (
	archgraph/nif => ../nif
	archgraph/storage => ../storage
)
