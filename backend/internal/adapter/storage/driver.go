package storage

// The pure-Go SQLite driver registers itself under the name "sqlite". It is
// isolated in this file so the remainder of the package compiles against the
// standard library's database/sql alone (the driver is only required at
// runtime, when sql.Open dials it).
import _ "modernc.org/sqlite"
