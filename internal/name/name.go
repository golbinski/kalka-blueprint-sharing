// Package name holds every string derived from the product name, so a rename
// touches one file (spec, preamble).
package name

const (
	// Product is the working name of the tool.
	Product = "kalka"

	// Binary is the executable name, without platform suffix.
	Binary = Product

	// PluginPrefix is prepended to a scheme to find its plugin (spec 10.2).
	PluginPrefix = Product + "-plugin-"

	// FrontmatterID is the frontmatter key holding the blueprint identity (spec 6.2).
	FrontmatterID = Product + "_id"

	// FrontmatterOwner is the frontmatter key naming the owner (spec 6.2).
	FrontmatterOwner = "owner"

	// HandoffSigContext prefixes the message signed in a handoff entry (spec 6.5).
	HandoffSigContext = Product + "-handoff-v1"
)
