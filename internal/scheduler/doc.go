// Package scheduler manages Docker networks and container deployments for godeploy.
// It applies CPU and memory limits, publishes ports on the PaaS bridge network,
// and performs a simple blue-green style rollout.
//
// Entry points: [New] to construct a [Scheduler], and [Scheduler.Deploy] or
// [Scheduler.DeployWithOptions] to run a deployment. The PaaS network is created
// lazily on the first deployment call (via [EnsurePaaSNetwork]) so that godeployd
// can boot and serve its HTTP healthcheck even when the Docker socket is not yet
// reachable (for example, distroless self-deploy before the operator binds
// /var/run/docker.sock). When a new container fails to reach the running state,
// [Scheduler.DeployWithOptions] tails its logs and embeds them in the returned
// error before the container is removed, to preserve the actual failure reason.
package scheduler
