// Package webhook parses and validates GitHub and GitLab push webhook HTTP requests.
//
// Use a [Parser] with the shared secret from configuration, then call [Parser.Parse]
// on each incoming POST. Successful parses yield an [Event] for the deployment pipeline.
package webhook
