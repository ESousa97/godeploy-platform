// Package proxy provides a Host-based reverse HTTP proxy backed by SQLite routes,
// plus a [Store] for persisting domain to upstream target (host:port) mappings.
// The pure-Go SQLite driver is registered via sqlite_driver.go.
//
// Use [New] to construct a [Proxy] and [Proxy.Run] to serve traffic; after changing
// routes in the database, call [Proxy.NotifyReload] to refresh the in-memory table.
package proxy
