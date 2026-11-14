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

	// AppID identifies kalka to the desktop: the launchd label on macOS,
	// the AppUserModelID of its notifications and the Run value on Windows.
	AppID = "com.github.golbinski." + Product

	// BackgroundBinary is the Windows build with no console window, used
	// when kalka starts at logon.
	BackgroundBinary = Product + "w"


	// EntrySigContext prefixes the message signed in any entry (spec 6.5,
	// v2): it covers the full digest of the entry, not the 48-bit id.
	EntrySigContext = Product + "-entry-v2"

	// TeamLogSigContext prefixes the message signed by a team log
	// operation (spec 19.1).
	TeamLogSigContext = Product + "-teamlog-v1"

	// InviteContext prefixes the invite token and the hashes derived from
	// its secret (spec 19.7).
	InviteContext = Product + "-invite-v1"

	// KeyIDContext prefixes the hash that derives a key peer id (spec 9.2).
	KeyIDContext = Product + "-key-v1"

	// KeyProofContext prefixes the message signed to register a key peer
	// id on `kalka rendezvous` (spec 9.6).
	KeyProofContext = Product + "-keyid-v1"
)
