package urlscheme

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// hkcu is the current user's hive.
type hkcu struct{}

func systemRegistry() Registry { return hkcu{} }

func (hkcu) String(key, value string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", ErrNotExist
	} else if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue(value)
	if errors.Is(err, registry.ErrNotExist) {
		return "", ErrNotExist
	}
	return s, err
}

func (hkcu) SetString(key, value, data string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(value, data)
}

func (hkcu) DeleteTree(key string) error { return deleteTree(registry.CURRENT_USER, key) }

// deleteTree removes key and its subkeys, deepest first.
func deleteTree(root registry.Key, key string) error {
	k, err := registry.OpenKey(root, key, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	subs, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return err
	}
	for _, s := range subs {
		if err := deleteTree(root, key+`\`+s); err != nil {
			return err
		}
	}
	err = registry.DeleteKey(root, key)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}
