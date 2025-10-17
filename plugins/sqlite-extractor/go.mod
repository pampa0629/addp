module github.com/addp/plugins/sqlite-extractor

go 1.23

require (
	github.com/addp/meta-extractor-sdk v1.0.0
	github.com/mattn/go-sqlite3 v1.14.24
)

replace github.com/addp/meta-extractor-sdk => ../../meta/sdk
