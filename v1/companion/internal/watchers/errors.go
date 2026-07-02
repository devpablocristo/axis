package watchers

import "github.com/devpablocristo/platform/errors/go/domainerr"

// ErrNotFound indica que el watcher o proposal no existe.
var ErrNotFound = domainerr.NotFound("not found")

// ErrWatcherDisabled indica que el watcher está deshabilitado.
var ErrWatcherDisabled = domainerr.Validation("watcher is disabled")
