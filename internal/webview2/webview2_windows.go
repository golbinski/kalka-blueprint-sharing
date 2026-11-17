package webview2

import "golang.org/x/sys/windows/registry"

// Has reads the runtime's version from the registry.
func Has() (string, bool) {
	var values []string
	for _, k := range Keys {
		root := registry.CURRENT_USER
		if k.Machine {
			root = registry.LOCAL_MACHINE
		}
		key, err := registry.OpenKey(root, k.Path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("pv")
		key.Close()
		if err == nil {
			values = append(values, v)
		}
	}
	return Version(values)
}
