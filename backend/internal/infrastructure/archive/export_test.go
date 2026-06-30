// export_test.go exposes internal helpers to the _test package for white-box
// testing without polluting the public API.
package archive

// NormalizeEntryPathExported is a thin re-export of normalizeEntryPath so
// nested_test.go (package archive_test) can call it.
func NormalizeEntryPathExported(p string) (string, error) {
	return normalizeEntryPath(p)
}
