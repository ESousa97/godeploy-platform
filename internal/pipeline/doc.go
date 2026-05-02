// Package pipeline orchestrates clone, image build, container deploy, HTTP
// health checks, and SQLite-backed route updates for the godeploy mini-PaaS.
//
// Create a [Runner] with [New] and a populated [Config], then invoke [Runner.Run]
// for each deployment request ([RunRequest]).
package pipeline
