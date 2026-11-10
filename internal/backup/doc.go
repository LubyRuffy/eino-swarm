// Package backup writes and restores a .zwai-backup archive.
//
// The live engine passes its open SQLite connection. VACUUM INTO and ATTACH
// run on that connection: the pool has one connection, and a second process
// must not open zwai.db while the engine holds it.
package backup
