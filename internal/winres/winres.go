// Package winres writes the Windows resources of kalka-desktop.exe: its
// icon, as a COFF object (.syso) that the Go linker embeds when it builds
// the package for Windows. Explorer, the Start menu shortcut, the taskbar
// and the window class all take the icon from these resources; without
// them Windows shows its default application icon.
//
// It also reads the icon group back out of a built executable, so the
// release can check that the linker kept it.
package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Machines of the COFF objects written.
const (
	AMD64 = pe.IMAGE_FILE_MACHINE_AMD64
	ARM64 = pe.IMAGE_FILE_MACHINE_ARM64
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	langEnUS    = 0x0409

	// GroupIDs are the ids the icon group is stored under: 3, which the
	// Wails runtime loads for the window and notifications, and
	// IDI_APPLICATION (32512), which LoadIcon finds for the window class.
	// Explorer's icon index 0, used by shortcuts, is the lowest id: 3.
	groupMain = 3
	groupApp  = 32512
)

// GroupIDs are the resource ids of the icon group.
var GroupIDs = []uint16{groupMain, groupApp}

// Image is one size of the icon, as PNG data, which Windows accepts in
// icon resources at any size.
type Image struct {
	Size int // width and height in pixels, 1 to 256
	PNG  []byte
}

// IconObject is a COFF object for machine holding the images as one icon
// group, stored under each of GroupIDs.
func IconObject(machine uint16, images []Image) ([]byte, error) {
	var reloc uint16
	switch machine {
	case AMD64:
		reloc = 3 // IMAGE_REL_AMD64_ADDR32NB
	case ARM64:
		reloc = 2 // IMAGE_REL_ARM64_ADDR32NB
	default:
		return nil, fmt.Errorf("machine %#x not supported", machine)
	}
	if len(images) == 0 {
		return nil, errors.New("no images")
	}
	imgs := append([]Image(nil), images...)
	sort.Slice(imgs, func(i, j int) bool { return imgs[i].Size < imgs[j].Size })

	// The group directory: GRPICONDIR, then one GRPICONDIRENTRY per image
	// naming its RT_ICON id (1, 2, ...).
	var group bytes.Buffer
	w16 := func(b *bytes.Buffer, v uint16) { _ = binary.Write(b, binary.LittleEndian, v) }
	w32 := func(b *bytes.Buffer, v uint32) { _ = binary.Write(b, binary.LittleEndian, v) }
	w16(&group, 0)
	w16(&group, 1)
	w16(&group, uint16(len(imgs)))
	for i, im := range imgs {
		if im.Size < 1 || im.Size > 256 {
			return nil, fmt.Errorf("icon size %d out of range", im.Size)
		}
		dim := byte(im.Size) // 256 is stored as 0
		group.Write([]byte{dim, dim, 0, 0})
		w16(&group, 1)  // planes
		w16(&group, 32) // bits per pixel
		w32(&group, uint32(len(im.PNG)))
		w16(&group, uint16(i+1))
	}

	// Leaves in directory order: RT_ICON 1..n, then RT_GROUP_ICON by id.
	type leaf struct {
		typ, id uint16
		data    []byte
	}
	var leaves []leaf
	for i, im := range imgs {
		leaves = append(leaves, leaf{rtIcon, uint16(i + 1), im.PNG})
	}
	for _, id := range GroupIDs {
		leaves = append(leaves, leaf{rtGroupIcon, id, group.Bytes()})
	}
	types := []uint16{rtIcon, rtGroupIcon}
	idsOf := func(t uint16) (out []leaf) {
		for _, l := range leaves {
			if l.typ == t {
				out = append(out, l)
			}
		}
		return out
	}

	// Layout: the root directory, one directory per type, one language
	// directory per resource, the data entries, then the data.
	const dirSize, entrySize, dataEntrySize = 16, 8, 16
	off := dirSize + entrySize*len(types)
	typeDir := map[uint16]int{}
	for _, t := range types {
		typeDir[t] = off
		off += dirSize + entrySize*len(idsOf(t))
	}
	langDir := make([]int, len(leaves))
	for i := range leaves {
		langDir[i] = off
		off += dirSize + entrySize
	}
	dataEntry := make([]int, len(leaves))
	for i := range leaves {
		dataEntry[i] = off
		off += dataEntrySize
	}
	dataAt := make([]int, len(leaves))
	for i, l := range leaves {
		off = (off + 7) &^ 7
		dataAt[i] = off
		off += len(l.data)
	}
	size := (off + 7) &^ 7

	sec := make([]byte, size)
	put16 := func(at int, v uint16) { binary.LittleEndian.PutUint16(sec[at:], v) }
	put32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(sec[at:], v) }
	dir := func(at, n int) { put16(at+14, uint16(n)) } // id entries only; the rest stays zero
	const sub = 0x80000000

	dir(0, len(types))
	for i, t := range types {
		put32(dirSize+entrySize*i, uint32(t))
		put32(dirSize+entrySize*i+4, sub|uint32(typeDir[t]))
	}
	for _, t := range types {
		at := typeDir[t]
		ls := idsOf(t)
		dir(at, len(ls))
		for j, l := range ls {
			for i := range leaves {
				if leaves[i].typ == l.typ && leaves[i].id == l.id {
					put32(at+dirSize+entrySize*j, uint32(l.id))
					put32(at+dirSize+entrySize*j+4, sub|uint32(langDir[i]))
				}
			}
		}
	}
	for i, l := range leaves {
		dir(langDir[i], 1)
		put32(langDir[i]+dirSize, langEnUS)
		put32(langDir[i]+dirSize+4, uint32(dataEntry[i]))
		put32(dataEntry[i], uint32(dataAt[i])) // relocated to an RVA by the linker
		put32(dataEntry[i]+4, uint32(len(l.data)))
		copy(sec[dataAt[i]:], l.data)
	}

	// The object: file header, one .rsrc section, its data, one
	// relocation per data entry, the section symbol, an empty string table.
	const fileHeader, sectionHeader, relocSize = 20, 40, 10
	rawAt := fileHeader + sectionHeader
	relocAt := rawAt + len(sec)
	symAt := relocAt + relocSize*len(leaves)
	var o bytes.Buffer
	w16(&o, machine)
	w16(&o, 1) // sections
	w32(&o, 0) // time stamp: none, so builds are reproducible
	w32(&o, uint32(symAt))
	w32(&o, 1) // symbols
	w16(&o, 0) // optional header
	w16(&o, 0) // characteristics
	o.WriteString(".rsrc\x00\x00\x00")
	w32(&o, 0) // virtual size
	w32(&o, 0) // virtual address
	w32(&o, uint32(len(sec)))
	w32(&o, uint32(rawAt))
	w32(&o, uint32(relocAt))
	w32(&o, 0) // line numbers
	w16(&o, uint16(len(leaves)))
	w16(&o, 0)
	w32(&o, pe.IMAGE_SCN_CNT_INITIALIZED_DATA|pe.IMAGE_SCN_MEM_READ)
	o.Write(sec)
	for i := range leaves {
		w32(&o, uint32(dataEntry[i]))
		w32(&o, 0) // symbol 0: .rsrc
		w16(&o, reloc)
	}
	o.WriteString(".rsrc\x00\x00\x00")
	w32(&o, 0)     // value
	w16(&o, 1)     // section number
	w16(&o, 0)     // type
	o.WriteByte(3) // IMAGE_SYM_CLASS_STATIC
	o.WriteByte(0) // aux symbols
	w32(&o, 4)     // string table size, itself included
	return o.Bytes(), nil
}

// GroupIcon reads the icon group with id out of a Windows executable: its
// images as stored, smallest first.
func GroupIcon(exe []byte, id uint16) ([]Image, error) {
	f, err := pe.NewFile(bytes.NewReader(exe))
	if err != nil {
		return nil, err
	}
	s := f.Section(".rsrc")
	if s == nil {
		return nil, errors.New("no .rsrc section")
	}
	sec, err := s.Data()
	if err != nil {
		return nil, err
	}
	return groupIcon(sec, int(s.VirtualAddress), id)
}

// ObjectIcon reads the icon group with id out of an object IconObject
// wrote, before linking: its data entries hold offsets into the section.
func ObjectIcon(obj []byte, id uint16) ([]Image, error) {
	f, err := pe.NewFile(bytes.NewReader(obj))
	if err != nil {
		return nil, err
	}
	s := f.Section(".rsrc")
	if s == nil {
		return nil, errors.New("no .rsrc section")
	}
	sec, err := s.Data()
	if err != nil {
		return nil, err
	}
	return groupIcon(sec, 0, id)
}

// groupIcon walks a resource section whose data entries hold base plus
// the data's offset in sec.
func groupIcon(sec []byte, base int, id uint16) ([]Image, error) {
	u16 := func(at int) (uint16, error) {
		if at < 0 || at+2 > len(sec) {
			return 0, errors.New("resource directory out of range")
		}
		return binary.LittleEndian.Uint16(sec[at:]), nil
	}
	u32 := func(at int) (uint32, error) {
		if at < 0 || at+4 > len(sec) {
			return 0, errors.New("resource directory out of range")
		}
		return binary.LittleEndian.Uint32(sec[at:]), nil
	}
	// find walks one directory level to the entry with id.
	find := func(dir int, id uint32) (uint32, error) {
		named, err := u16(dir + 12)
		if err != nil {
			return 0, err
		}
		ids, err := u16(dir + 14)
		if err != nil {
			return 0, err
		}
		for i := 0; i < int(named)+int(ids); i++ {
			name, err := u32(dir + 16 + 8*i)
			if err != nil {
				return 0, err
			}
			if name == id {
				return u32(dir + 16 + 8*i + 4)
			}
		}
		return 0, fmt.Errorf("resource %d not found", id)
	}
	// leaf finds type/id, in the first language listed.
	leaf := func(typ, id uint32) ([]byte, error) {
		t, err := find(0, typ)
		if err != nil {
			return nil, err
		}
		r, err := find(int(t&0x7fffffff), id)
		if err != nil {
			return nil, err
		}
		langs := int(r & 0x7fffffff)
		e, err := u32(langs + 16 + 4)
		if err != nil {
			return nil, err
		}
		rva, err := u32(int(e))
		if err != nil {
			return nil, err
		}
		n, err := u32(int(e) + 4)
		if err != nil {
			return nil, err
		}
		at := int(rva) - base
		if at < 0 || at+int(n) > len(sec) {
			return nil, fmt.Errorf("resource %d/%d data out of range", typ, id)
		}
		return sec[at : at+int(n)], nil
	}
	g, err := leaf(rtGroupIcon, uint32(id))
	if err != nil {
		return nil, err
	}
	if len(g) < 6 || binary.LittleEndian.Uint16(g[2:]) != 1 {
		return nil, errors.New("not an icon group")
	}
	n := int(binary.LittleEndian.Uint16(g[4:]))
	if len(g) < 6+14*n {
		return nil, errors.New("icon group truncated")
	}
	var out []Image
	for i := 0; i < n; i++ {
		e := g[6+14*i:]
		size := int(e[0])
		if size == 0 {
			size = 256
		}
		data, err := leaf(rtIcon, uint32(binary.LittleEndian.Uint16(e[12:])))
		if err != nil {
			return nil, err
		}
		out = append(out, Image{Size: size, PNG: data})
	}
	return out, nil
}
