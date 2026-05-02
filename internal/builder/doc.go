// Package builder drives Docker image builds from a local application directory.
// It uses the Docker Engine HTTP API to stream a tar build context. When the tree
// has no Dockerfile, it selects an embedded template based on [detector.Detect].
//
// Construct a [Builder] with [New], then call [Builder.Build] with [Options].
package builder
