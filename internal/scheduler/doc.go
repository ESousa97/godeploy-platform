// Package scheduler manages Docker networks and container deployments for godeploy.
// It applies CPU and memory limits, publishes ports on the PaaS bridge network,
// and performs a simple blue-green style rollout.
//
// Entry points: [New] to construct a [Scheduler], [EnsurePaaSNetwork] to guarantee
// the Docker network exists, and [Scheduler.Deploy] or [Scheduler.DeployWithOptions]
// to run a deployment.
package scheduler
