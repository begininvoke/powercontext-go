// Package work models the human-agent work continuity loop.
//
// Work records are immutable, versioned Source payloads. They deliberately do
// not form another Artifact family: committed Handoffs remain the durable
// authority while contracts, boundaries, acknowledgements, and outcomes form
// an ordered evidence journal around them.
package work
