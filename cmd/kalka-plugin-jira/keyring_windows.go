package main

// The Windows Credential Manager: a generic credential per site.

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32     = windows.NewLazySystemDLL("advapi32.dll")
	procWrite    = advapi32.NewProc("CredWriteW")
	procRead     = advapi32.NewProc("CredReadW")
	procDelete   = advapi32.NewProc("CredDeleteW")
	procCredFree = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type credManager struct{}

func systemKeyring() keyring { return credManager{} }

func target(account string) (*uint16, error) {
	return windows.UTF16PtrFromString(service + ":" + account)
}

func (credManager) set(account, secret string) error {
	t, err := target(account)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString(service)
	blob := []byte(secret)
	if len(blob) == 0 {
		return errors.New("empty token")
	}
	c := credential{Type: credTypeGeneric, TargetName: t, CredentialBlobSize: uint32(len(blob)), CredentialBlob: &blob[0],
		Persist: credPersistLocalMachine, UserName: user}
	if r, _, err := procWrite.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return err
	}
	return nil
}

func (credManager) get(account string) (string, error) {
	t, err := target(account)
	if err != nil {
		return "", err
	}
	var p *credential
	if r, _, err := procRead.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p))); r == 0 {
		return "", err
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(p)))
	return string(unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize)), nil
}

func (credManager) del(account string) error {
	t, err := target(account)
	if err != nil {
		return err
	}
	if r, _, err := procDelete.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0); r == 0 {
		return err
	}
	return nil
}
