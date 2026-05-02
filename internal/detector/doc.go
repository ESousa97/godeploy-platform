// Package detector inspects a project root on disk and chooses a deployment
// runtime from conventional marker files (Dockerfile, go.mod, package.json, etc.).
//
// The primary API is [Detect], which returns a [Result] with the chosen [Runtime]
// and evidence paths.
package detector
