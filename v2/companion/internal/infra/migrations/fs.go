package migrations

import "os"

const Dir = "migrations"

// Files reads Companion SQL migrations from the service runtime filesystem.
var Files = os.DirFS(".")
