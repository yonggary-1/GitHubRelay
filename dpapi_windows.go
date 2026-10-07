//go:build windows

package main

import (
	"errors"
	"unsafe"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

const cryptprotectUIForbidden = 0x1

// dpapi encrypts with the current Windows user's key (CryptProtectData).
type dpapi struct{}

var entropy = []byte("GitHubRelay token v1")

func blobOf(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (dpapi) Protect(in []byte) ([]byte, error) {
	var out dataBlob
	r, _, err := pCryptProtectData.Call(
		uintptr(unsafe.Pointer(blobOf(in))), 0, uintptr(unsafe.Pointer(blobOf(entropy))),
		0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}

func (dpapi) Unprotect(in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("empty")
	}
	var out dataBlob
	r, _, err := pCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(blobOf(in))), 0, uintptr(unsafe.Pointer(blobOf(entropy))),
		0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}
