//go:build windows

package vault

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

const uiForbidden = 0x1

func blob(data []byte) *windows.DataBlob {
	if len(data) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}
func protect(data []byte) ([]byte, error) {
	var out windows.DataBlob
	description, _ := windows.UTF16PtrFromString("TDL Media Telegram session")
	if err := windows.CryptProtectData(blob(data), description, nil, 0, nil, uiForbidden, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	result := make([]byte, out.Size)
	copy(result, unsafe.Slice(out.Data, out.Size))
	return result, nil
}
func unprotect(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(data), nil, nil, 0, nil, uiForbidden, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	result := make([]byte, out.Size)
	copy(result, unsafe.Slice(out.Data, out.Size))
	return result, nil
}
