package vfs

// LogCloseError is called when closing a file fails (e.g. in finalizer).
// Default is a no-op; callers may set it to log or report errors.
var LogCloseError = func(error) {}
