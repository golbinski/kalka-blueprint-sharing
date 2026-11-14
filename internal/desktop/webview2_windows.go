package desktop

import "golang.org/x/sys/windows/registry"

// HasWebView2 reads the runtime's version from the registry.
func HasWebView2() (string, bool) {
	var values []string
	for _, k := range WebView2Keys {
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
	return WebView2Version(values)
}
