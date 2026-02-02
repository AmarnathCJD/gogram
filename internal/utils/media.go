package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type ProbeInfo struct {
	FastStart bool
	Timescale uint32
	Duration  uint64
}

const (
	SmallHeaderSize = 8
	LargeHeaderSize = 16
	LengthUnlimited = math.MaxUint32
)

type Box struct {
	BaseCustomFieldObject
}

// GetVersion returns the box version
func (box *Box) GetVersion() uint8 {
	return 0
}

// SetVersion sets the box version
func (box *Box) SetVersion(uint8) {
}

// GetFlags returns the flags
func (box *Box) GetFlags() uint32 {
	return 0x000000
}

// CheckFlag checks the flag status
func (box *Box) CheckFlag(flag uint32) bool {
	return true
}

// SetFlags sets the flags
func (box *Box) SetFlags(uint32) {
}

// AddFlag adds the flag
func (box *Box) AddFlag(flag uint32) {
}

// RemoveFlag removes the flag
func (box *Box) RemoveFlag(flag uint32) {
}

// GetVersion returns the box version
func (box *FullBox) GetVersion() uint8 {
	return box.Version
}

// SetVersion sets the box version
func (box *FullBox) SetVersion(version uint8) {
	box.Version = version
}

// GetFlags returns the flags
func (box *FullBox) GetFlags() uint32 {
	flag := uint32(box.Flags[0]) << 16
	flag ^= uint32(box.Flags[1]) << 8
	flag ^= uint32(box.Flags[2])
	return flag
}

// CheckFlag checks the flag status
func (box *FullBox) CheckFlag(flag uint32) bool {
	return box.GetFlags()&flag != 0
}

// SetFlags sets the flags
func (box *FullBox) SetFlags(flags uint32) {
	box.Flags[0] = byte(flags >> 16)
	box.Flags[1] = byte(flags >> 8)
	box.Flags[2] = byte(flags)
}

// AddFlag adds the flag
func (box *FullBox) AddFlag(flag uint32) {
	box.SetFlags(box.GetFlags() | flag)
}

// RemoveFlag removes the flag
func (box *FullBox) RemoveFlag(flag uint32) {
	box.SetFlags(box.GetFlags() & (^flag))
}

type BoxType [4]byte

func StrToBoxType(code string) BoxType {
	if len(code) != 4 {
		return BoxType{0x00, 0x00, 0x00, 0x00}
	}
	return BoxType{code[0], code[1], code[2], code[3]}
}

func (boxType BoxType) String() string {
	if isPrintable(boxType[0]) && isPrintable(boxType[1]) && isPrintable(boxType[2]) && isPrintable(boxType[3]) {
		s := string([]byte{boxType[0], boxType[1], boxType[2], boxType[3]})
		s = strings.ReplaceAll(s, string([]byte{0xa9}), "(c)")
		return s
	}
	return fmt.Sprintf("0x%02x%02x%02x%02x", boxType[0], boxType[1], boxType[2], boxType[3])
}

func isASCII(c byte) bool {
	return c >= 0x20 && c <= 0x7e
}

func isPrintable(c byte) bool {
	return isASCII(c) || c == 0xa9
}

var boxTypeAny = BoxType{0x00, 0x00, 0x00, 0x00}

func BoxTypeAny() BoxType {
	return boxTypeAny
}

type boxDef struct {
	dataType reflect.Type
	versions []uint8
	isTarget func(Context) bool
	fields   []*field
}

var boxMap = make(map[BoxType][]boxDef, 64)

func (boxType BoxType) getBoxDef(ctx Context) *boxDef {
	boxDefs := boxMap[boxType]
	for i := len(boxDefs) - 1; i >= 0; i-- {
		boxDef := &boxDefs[i]
		if boxDef.isTarget == nil || boxDef.isTarget(ctx) {
			return boxDef
		}
	}
	return nil
}

func (boxType BoxType) IsSupportedVersion(ver uint8, ctx Context) bool {
	boxDef := boxType.getBoxDef(ctx)
	if boxDef == nil {
		return false
	}
	if len(boxDef.versions) == 0 {
		return true
	}
	for _, sver := range boxDef.versions {
		if ver == sver {
			return true
		}
	}
	return false
}

func (boxType BoxType) New(ctx Context) (IBox, error) {
	boxDef := boxType.getBoxDef(ctx)
	if boxDef == nil {
		return nil, fmt.Errorf("box type not found: %s", boxType.String())
	}

	box, ok := reflect.New(boxDef.dataType).Interface().(IBox)
	if !ok {
		return nil, fmt.Errorf("box type not implements IBox interface: %s", boxType.String())
	}

	anyTypeBox, ok := box.(IAnyType)
	if ok {
		anyTypeBox.SetType(boxType)
	}

	return box, nil
}

type IAnyType interface {
	IBox
	SetType(BoxType)
}

type BoxPath []BoxType

func (lhs BoxPath) compareWith(rhs BoxPath) (forwardMatch bool, match bool) {
	if len(lhs) > len(rhs) {
		return false, false
	}
	for i := 0; i < len(lhs); i++ {
		if !lhs[i].MatchWith(rhs[i]) {
			return false, false
		}
	}
	if len(lhs) < len(rhs) {
		return true, false
	}
	return false, true
}

func (lhs BoxType) MatchWith(rhs BoxType) bool {
	if lhs == boxTypeAny || rhs == boxTypeAny {
		return true
	}
	return lhs == rhs
}

type Context struct {
	IsQuickTimeCompatible bool
	UnderWave             bool
	UnderIlst             bool
	UnderIlstMeta         bool
	UnderIlstFreeMeta     bool
	UnderUdta             bool
}

// BoxInfo has common infomations of box
type BoxInfo struct {
	Offset      uint64
	Size        uint64
	HeaderSize  uint64
	Type        BoxType
	ExtendToEOF bool
	Context
}

func AddBoxDef(payload IBox, versions ...uint8) {
	boxMap[payload.GetType()] = append(boxMap[payload.GetType()], boxDef{
		dataType: reflect.TypeOf(payload).Elem(),
		versions: versions,
		fields:   buildFields(payload),
	})
}

func init() {
	AddBoxDef(&Mvhd{}, 0, 1)
	AddBoxDef(&Moov{})
}

// Mvhd is ISOBMFF mvhd box type
type Mvhd struct {
	FullBox            `mp4:"0,extend"`
	CreationTimeV0     uint32    `mp4:"1,size=32,ver=0"`
	ModificationTimeV0 uint32    `mp4:"2,size=32,ver=0"`
	CreationTimeV1     uint64    `mp4:"3,size=64,ver=1"`
	ModificationTimeV1 uint64    `mp4:"4,size=64,ver=1"`
	Timescale          uint32    `mp4:"5,size=32"`
	DurationV0         uint32    `mp4:"6,size=32,ver=0"`
	DurationV1         uint64    `mp4:"7,size=64,ver=1"`
	Rate               int32     `mp4:"8,size=32"`
	Volume             int16     `mp4:"9,size=16"`
	Reserved           int16     `mp4:"10,size=16,const=0"`
	Reserved2          [2]uint32 `mp4:"11,size=32,const=0"`
	Matrix             [9]int32  `mp4:"12,size=32,hex"`
	PreDefined         [6]int32  `mp4:"13,size=32"`
	NextTrackID        uint32    `mp4:"14,size=32"`
}

func (*Mvhd) AddFlag(flag uint32) {}

// GetType returns the BoxType
func (*Mvhd) GetType() BoxType {
	return BoxTypeMvhd()
}

func (mvhd *Mvhd) GetCreationTime() uint64 {
	switch mvhd.GetVersion() {
	case 0:
		return uint64(mvhd.CreationTimeV0)
	case 1:
		return mvhd.CreationTimeV1
	default:
		return 0
	}
}

func (mvhd *Mvhd) GetModificationTime() uint64 {
	switch mvhd.GetVersion() {
	case 0:
		return uint64(mvhd.ModificationTimeV0)
	case 1:
		return mvhd.ModificationTimeV1
	default:
		return 0
	}
}

func (mvhd *Mvhd) GetDuration() uint64 {
	switch mvhd.GetVersion() {
	case 0:
		return uint64(mvhd.DurationV0)
	case 1:
		return mvhd.DurationV1
	default:
		return 0
	}
}

// GetRate returns value of rate as float64
func (mvhd *Mvhd) GetRate() float64 {
	return float64(mvhd.Rate) / (1 << 16)
}

// GetRateInt returns value of rate as int16
func (mvhd *Mvhd) GetRateInt() int16 {
	return int16(mvhd.Rate >> 16)
}

type Moov struct {
	Box
}

// GetType returns the BoxType
func (*Moov) GetType() BoxType {
	return BoxTypeMoov()
}

func ParseDuration(file string) (int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var r io.ReadSeeker = f
	probeInfo := &ProbeInfo{}
	bis, err := ExtractBoxes(r, nil, []BoxPath{
		{BoxTypeMoov(), BoxTypeMvhd()},
	})
	if err != nil {
		return 0, err
	}
	var mdatAppeared bool
	for _, bi := range bis {
		switch bi.Type {
		case BoxTypeMvhd():
			var mvhd Mvhd
			if _, err := bi.SeekToPayload(r); err != nil {
				return 0, err
			}
			if _, err := Unmarshal(r, bi.Size-bi.HeaderSize, &mvhd, bi.Context); err != nil {
				return 0, err
			}
			probeInfo.Timescale = mvhd.Timescale
			if mvhd.GetVersion() == 0 {
				probeInfo.Duration = uint64(mvhd.DurationV0)
			} else {
				probeInfo.Duration = mvhd.DurationV1
			}
		case BoxTypeMoov():
			probeInfo.FastStart = !mdatAppeared
		}
	}
	return int64(probeInfo.Duration), nil
}

type ReadHandle struct {
	Params      []interface{}
	BoxInfo     BoxInfo
	Path        BoxPath
	ReadPayload func() (box IBox, n uint64, err error)
	ReadData    func(io.Writer) (n uint64, err error)
	Expand      func(params ...interface{}) (vals []interface{}, err error)
}

type ReadHandler func(handle *ReadHandle) (val interface{}, err error)

func ExtractBoxes(r io.ReadSeeker, parent *BoxInfo, paths []BoxPath) ([]*BoxInfo, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	for i := range paths {
		if len(paths[i]) == 0 {
			return nil, fmt.Errorf("invalid box path: %v", paths[i])
		}
	}

	boxes := make([]*BoxInfo, 0, 8)

	handler := func(handle *ReadHandle) (interface{}, error) {
		path := handle.Path
		if parent != nil {
			path = path[1:]
		}
		if handle.BoxInfo.Type == BoxTypeAny() {
			return nil, nil
		}
		fm, m := matchPath(paths, path)
		if m {
			boxes = append(boxes, &handle.BoxInfo)
		}

		if fm {
			if _, err := handle.Expand(); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}

	if parent != nil {
		_, err := ReadBoxStructureFromInternal(r, parent, handler)
		return boxes, err
	}
	_, err := ReadBoxStructure(r, handler)
	return boxes, err
}

func BoxTypeMoov() BoxType { return StrToBoxType("moov") }
func BoxTypeMvhd() BoxType { return StrToBoxType("mvhd") }

func matchPath(paths []BoxPath, path BoxPath) (forwardMatch bool, match bool) {
	for i := range paths {
		fm, m := path.compareWith(paths[i])
		forwardMatch = forwardMatch || fm
		match = match || m
	}
	return
}

func ReadBoxStructure(r io.ReadSeeker, handler ReadHandler, params ...interface{}) ([]interface{}, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return readBoxStructure(r, 0, true, nil, Context{}, handler, params)
}

func ReadBoxStructureFromInternal(r io.ReadSeeker, bi *BoxInfo, handler ReadHandler, params ...interface{}) (interface{}, error) {
	return readBoxStructureFromInternal(r, bi, nil, handler, params)
}

type Ftyp struct {
	Box
	MajorBrand       [4]byte               `mp4:"0,size=8,string"`
	MinorVersion     uint32                `mp4:"1,size=32"`
	CompatibleBrands []CompatibleBrandElem `mp4:"2,size=32"` // reach to end of the box
}

type CompatibleBrandElem struct {
	CompatibleBrand [4]byte `mp4:"0,size=8,string"`
}

func (ftyp *Ftyp) HasCompatibleBrand(cb [4]byte) bool {
	for i := range ftyp.CompatibleBrands {
		if ftyp.CompatibleBrands[i].CompatibleBrand == cb {
			return true
		}
	}
	return false
}

func (box *BaseCustomFieldObject) GetFieldLength(string, Context) uint { return 0 }

func (box *BaseCustomFieldObject) GetFieldSize(string, Context) uint { return 0 }

func (box *BaseCustomFieldObject) IsOptFieldEnabled(string, Context) bool {
	return false
}

func (*BaseCustomFieldObject) BeforeUnmarshal(io.ReadSeeker, uint64, Context) (uint64, bool, error) {
	return 0, false, nil
}

func (*BaseCustomFieldObject) OnReadField(string, ReadSeeker, uint64, Context) (uint64, bool, error) {
	return 0, false, nil
}

func readBoxStructureFromInternal(r io.ReadSeeker, bi *BoxInfo, path BoxPath, handler ReadHandler, params []interface{}) (interface{}, error) {
	if _, err := bi.SeekToPayload(r); err != nil {
		return nil, err
	}
	// check comatible-brands

	ctx := bi.Context

	newPath := make(BoxPath, len(path)+1)
	copy(newPath, path)
	newPath[len(path)] = bi.Type

	h := &ReadHandle{
		Params:  params,
		BoxInfo: *bi,
		Path:    newPath,
	}

	var childrenOffset uint64

	h.ReadPayload = func() (IBox, uint64, error) {
		if _, err := bi.SeekToPayload(r); err != nil {
			return nil, 0, err
		}

		box, n, err := UnmarshalAny(r, bi.Type, bi.Size-bi.HeaderSize, bi.Context)
		if err != nil {
			return nil, 0, err
		}
		childrenOffset = bi.Offset + bi.HeaderSize + n
		return box, n, nil
	}

	h.ReadData = func(w io.Writer) (uint64, error) {
		if _, err := bi.SeekToPayload(r); err != nil {
			return 0, err
		}

		size := bi.Size - bi.HeaderSize
		if _, err := io.CopyN(w, r, int64(size)); err != nil {
			return 0, err
		}
		return size, nil
	}

	h.Expand = func(params ...interface{}) ([]interface{}, error) {
		if childrenOffset == 0 {
			if _, err := bi.SeekToPayload(r); err != nil {
				return nil, err
			}

			_, n, err := UnmarshalAny(r, bi.Type, bi.Size-bi.HeaderSize, bi.Context)
			if err != nil {
				return nil, err
			}
			childrenOffset = bi.Offset + bi.HeaderSize + n
		} else {
			if _, err := r.Seek(int64(childrenOffset), io.SeekStart); err != nil {
				return nil, err
			}
		}

		childrenSize := bi.Offset + bi.Size - childrenOffset
		return readBoxStructure(r, childrenSize, false, newPath, ctx, handler, params)
	}

	if val, err := handler(h); err != nil {
		return nil, err
	} else if _, err := bi.SeekToEnd(r); err != nil {
		return nil, err
	} else {
		return val, nil
	}
}

func readBoxStructure(r io.ReadSeeker, totalSize uint64, isRoot bool, path BoxPath, ctx Context, handler ReadHandler, params []interface{}) ([]interface{}, error) {
	vals := make([]interface{}, 0, 8)

	for isRoot || totalSize >= SmallHeaderSize {
		bi, err := ReadBoxInfo(r)
		if isRoot && err == io.EOF {
			return vals, nil
		} else if err != nil {
			return nil, err
		}

		if !isRoot && bi.Size > totalSize {
			return nil, fmt.Errorf("too large box size: type=%s, size=%d, actualBufSize=%d", bi.Type.String(), bi.Size, totalSize)
		}
		totalSize -= bi.Size

		bi.Context = ctx

		val, err := readBoxStructureFromInternal(r, bi, path, handler, params)
		if err != nil {
			return nil, err
		}
		vals = append(vals, val)

		if bi.IsQuickTimeCompatible {
			ctx.IsQuickTimeCompatible = true
		}
	}
	if totalSize != 0 && !ctx.IsQuickTimeCompatible {
		return nil, fmt.Errorf("invalid box size: actualBufSize=%d", totalSize)
	}

	return vals, nil
}

func ReadBoxInfo(r io.ReadSeeker) (*BoxInfo, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	bi := &BoxInfo{
		Offset: uint64(offset),
	}

	// read 8 bytes
	buf := bytes.NewBuffer(make([]byte, 0, SmallHeaderSize))
	if _, err := io.CopyN(buf, r, SmallHeaderSize); err != nil {
		return nil, err
	}
	bi.HeaderSize += SmallHeaderSize

	// pick size and type
	data := buf.Bytes()
	bi.Size = uint64(binary.BigEndian.Uint32(data))
	bi.Type = BoxType{data[4], data[5], data[6], data[7]}

	if bi.Size == 0 {
		// box extends to end of file
		offsetEOF, err := r.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, err
		}
		bi.Size = uint64(offsetEOF) - bi.Offset
		bi.ExtendToEOF = true
		if _, err := bi.SeekToPayload(r); err != nil {
			return nil, err
		}

	} else if bi.Size == 1 {
		// read more 8 bytes
		buf.Reset()
		if _, err := io.CopyN(buf, r, LargeHeaderSize-SmallHeaderSize); err != nil {
			return nil, err
		}
		bi.HeaderSize += LargeHeaderSize - SmallHeaderSize
		bi.Size = binary.BigEndian.Uint64(buf.Bytes())
	}

	return bi, nil
}

func (bi *BoxInfo) SeekToStart(s io.Seeker) (int64, error) {
	return s.Seek(int64(bi.Offset), io.SeekStart)
}

func (bi *BoxInfo) SeekToPayload(s io.Seeker) (int64, error) {
	return s.Seek(int64(bi.Offset+bi.HeaderSize), io.SeekStart)
}

func (bi *BoxInfo) SeekToEnd(s io.Seeker) (int64, error) {
	return s.Seek(int64(bi.Offset+bi.Size), io.SeekStart)
}

var ErrUnsupportedBoxVersion = errors.New("unsupported box version")

type unmarshaller struct {
	reader ReadSeeker
	dst    IBox
	size   uint64
	rbits  uint64
	ctx    Context
}

func UnmarshalAny(r io.ReadSeeker, boxType BoxType, payloadSize uint64, ctx Context) (box IBox, n uint64, err error) {
	dst, err := boxType.New(ctx)
	if err != nil {
		return nil, 0, err
	}
	n, err = Unmarshal(r, payloadSize, dst, ctx)
	return dst, n, err
}

func Unmarshal(r io.ReadSeeker, payloadSize uint64, dst IBox, ctx Context) (n uint64, err error) {
	boxDef := dst.GetType().getBoxDef(ctx)
	if boxDef == nil {
		return 0, fmt.Errorf("box type %s is not registered", dst.GetType())
	}

	v := reflect.ValueOf(dst).Elem()

	dst.SetVersion(math.MaxUint8)

	u := &unmarshaller{
		reader: NewReadSeeker(r),
		dst:    dst,
		size:   payloadSize,
		ctx:    ctx,
	}

	if n, override, err := dst.BeforeUnmarshal(r, payloadSize, u.ctx); err != nil {
		return 0, err
	} else if override {
		return n, nil
	} else {
		u.rbits = n * 8
	}

	sn, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	if err := u.unmarshalStruct(v, boxDef.fields); err != nil {
		if err == ErrUnsupportedBoxVersion {
			r.Seek(sn, io.SeekStart)
		}
		return 0, err
	}

	if u.rbits%8 != 0 {
		return 0, fmt.Errorf("box size is not multiple of 8 bits: type=%s, size=%d, bits=%d", dst.GetType().String(), u.size, u.rbits)
	}

	if u.rbits > u.size*8 {
		return 0, fmt.Errorf("overrun error: type=%s, size=%d, bits=%d", dst.GetType().String(), u.size, u.rbits)
	}

	return u.rbits / 8, nil
}

func (u *unmarshaller) unmarshal(v reflect.Value, fi *fieldInstance) error {
	switch v.Type().Kind() {
	case reflect.Struct:
		return u.unmarshalStructInternal(v, fi)
	case reflect.Array:
		return u.unmarshalArray(v, fi)
	case reflect.Slice:
		return u.unmarshalSlice(v, fi)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return u.unmarshalInt(v, fi)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return u.unmarshalUint(v, fi)
	default:
		return fmt.Errorf("unsupported type: %s", v.Type().Kind())
	}
}

func (u *unmarshaller) unmarshalStructInternal(v reflect.Value, fi *fieldInstance) error {
	if fi.size != 0 && fi.size%8 == 0 {
		u2 := *u
		u2.size = uint64(fi.size / 8)
		u2.rbits = 0
		if err := u2.unmarshalStruct(v, fi.children); err != nil {
			return err
		}
		u.rbits += u2.rbits
		if u2.rbits != uint64(fi.size) {
			return errors.New("invalid alignment")
		}
		return nil
	}
	return u.unmarshalStruct(v, fi.children)
}

func (u *unmarshaller) unmarshalStruct(v reflect.Value, fs []*field) error {
	for _, f := range fs {
		fi := resolveFieldInstance(f, u.dst, v, u.ctx)
		if !isTargetField(u.dst, fi, u.ctx) {
			continue
		}
		rbits, override, err := fi.cfo.OnReadField(f.name, u.reader, u.size*8-u.rbits, u.ctx)
		if err != nil {
			return err
		}
		u.rbits += rbits
		if override {
			continue
		}
		err = u.unmarshal(v.FieldByName(f.name), fi)
		if err != nil {
			return err
		}
		if v.FieldByName(f.name).Type() == reflect.TypeOf(FullBox{}) && !u.dst.GetType().IsSupportedVersion(u.dst.GetVersion(), u.ctx) {
			return ErrUnsupportedBoxVersion
		}
	}

	return nil
}

type BaseCustomFieldObject struct{}

type FullBox struct {
	BaseCustomFieldObject
	Version uint8   `mp4:"0,size=8"`
	Flags   [3]byte `mp4:"1,size=8"`
}

func (u *unmarshaller) unmarshalArray(v reflect.Value, fi *fieldInstance) error {
	size := v.Type().Size()
	for i := 0; i < int(size)/int(v.Type().Elem().Size()); i++ {
		err := u.unmarshal(v.Index(i), fi)
		if err != nil {
			return err
		}
	}
	return nil
}

func (u *unmarshaller) unmarshalSlice(v reflect.Value, fi *fieldInstance) error {
	var slice reflect.Value
	elemType := v.Type().Elem()

	length := uint64(fi.length)
	if fi.length == LengthUnlimited {
		if fi.size != 0 {
			left := (u.size)*8 - u.rbits
			if left%uint64(fi.size) != 0 {
				return errors.New("invalid alignment")
			}
			length = left / uint64(fi.size)
		} else {
			length = 0
		}
	}

	if length > math.MaxInt32 {
		return fmt.Errorf("out of memory: requestedSize=%d", length)
	}

	if fi.size != 0 && fi.size%8 == 0 && u.rbits%8 == 0 && elemType.Kind() == reflect.Uint8 && fi.size == 8 {
		totalSize := length * uint64(fi.size) / 8
		buf := bytes.NewBuffer(make([]byte, 0, totalSize))
		if _, err := io.CopyN(buf, u.reader, int64(totalSize)); err != nil {
			return err
		}
		slice = reflect.ValueOf(buf.Bytes())
		u.rbits += uint64(totalSize) * 8

	} else {
		slice = reflect.MakeSlice(v.Type(), 0, int(length))
		for i := 0; ; i++ {
			if fi.length != LengthUnlimited && uint(i) >= fi.length {
				break
			}
			if fi.length == LengthUnlimited && u.rbits >= u.size*8 {
				break
			}
			slice = reflect.Append(slice, reflect.Zero(elemType))
			if err := u.unmarshal(slice.Index(i), fi); err != nil {
				return err
			}
			if u.rbits > u.size*8 {
				return fmt.Errorf("failed to read array completely: fieldName=\"%s\"", fi.name)
			}
		}
	}

	v.Set(slice)
	return nil
}

func (u *unmarshaller) unmarshalInt(v reflect.Value, fi *fieldInstance) error {
	if fi.is(fieldVarint) {
		return fmt.Errorf("unsupported type: %s", v.Type().Kind())
	}

	if fi.size == 0 {
		return fmt.Errorf("size must not be zero: %s", fi.name)
	}

	data, err := u.reader.ReadBits(fi.size)
	if err != nil {
		return err
	}
	u.rbits += uint64(fi.size)

	signBit := false
	if len(data) > 0 {
		signMask := byte(0x01) << ((fi.size - 1) % 8)
		signBit = data[0]&signMask != 0
		if signBit {
			data[0] |= ^(signMask - 1)
		}
	}

	var val uint64
	if signBit {
		val = ^uint64(0)
	}
	for i := range data {
		val <<= 8
		val |= uint64(data[i])
	}
	v.SetInt(int64(val))
	return nil
}

func (u *unmarshaller) unmarshalUint(v reflect.Value, fi *fieldInstance) error {
	if fi.is(fieldVarint) {
		val, err := u.readUvarint()
		if err != nil {
			return err
		}
		v.SetUint(val)
		return nil
	}

	if fi.size == 0 {
		return fmt.Errorf("size must not be zero: %s", fi.name)
	}

	data, err := u.reader.ReadBits(fi.size)
	if err != nil {
		return err
	}
	u.rbits += uint64(fi.size)

	val := uint64(0)
	for i := range data {
		val <<= 8
		val |= uint64(data[i])
	}
	v.SetUint(val)

	return nil
}

func (u *unmarshaller) readUvarint() (uint64, error) {
	var val uint64
	for {
		octet, err := u.reader.ReadBits(8)
		if err != nil {
			return 0, err
		}
		u.rbits += 8

		val = (val << 7) + uint64(octet[0]&0x7f)

		if octet[0]&0x80 == 0 {
			return val, nil
		}
	}
}

type (
	fieldFlag uint16
)

const (
	fieldString        fieldFlag = 1 << iota // 0
	fieldExtend                              // 1
	fieldDec                                 // 2
	fieldHex                                 // 3
	fieldISO639_2                            // 4
	fieldUUID                                // 5
	fieldHidden                              // 6
	fieldOptDynamic                          // 7
	fieldVarint                              // 8
	fieldSizeDynamic                         // 9
	fieldLengthDynamic                       // 10
)

type field struct {
	children []*field
	name     string
	order    int
	optFlag  uint32
	nOptFlag uint32
	size     uint
	length   uint
	flags    fieldFlag
	version  uint8
	nVersion uint8
}

func (f *field) set(flag fieldFlag) {
	f.flags |= flag
}

func (f *field) is(flag fieldFlag) bool {
	return f.flags&flag != 0
}

func buildFields(box IImmutableBox) []*field {
	t := reflect.TypeOf(box).Elem()
	return buildFieldsStruct(t)
}

func buildFieldsStruct(t reflect.Type) []*field {
	fs := make([]*field, 0, 8)
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i).Type
		tag, ok := t.Field(i).Tag.Lookup("mp4")
		if !ok {
			continue
		}
		f := buildField(t.Field(i).Name, tag)
		f.children = buildFieldsAny(ft)
		fs = append(fs, f)
	}
	sort.SliceStable(fs, func(i, j int) bool {
		return fs[i].order < fs[j].order
	})
	return fs
}

func buildFieldsAny(t reflect.Type) []*field {
	switch t.Kind() {
	case reflect.Struct:
		return buildFieldsStruct(t)
	case reflect.Ptr, reflect.Array, reflect.Slice:
		return buildFieldsAny(t.Elem())
	default:
		return nil
	}
}

func buildField(fieldName string, tag string) *field {
	f := &field{
		name: fieldName,
	}
	tagMap := parseFieldTag(tag)
	for key, val := range tagMap {
		if val != "" {
			continue
		}
		if order, err := strconv.Atoi(key); err == nil {
			f.order = order
			break
		}
	}

	if val, contained := tagMap["string"]; contained {
		f.set(fieldString)
		if val == "c_p" {
			fmt.Fprint(os.Stderr, "gogram: string=c_p tag is deprecated!!!")
		}
	}
	if _, contained := tagMap["varint"]; contained {
		f.set(fieldVarint)
	}
	f.version = math.MaxUint8
	if val, contained := tagMap["ver"]; contained {
		ver, err := strconv.Atoi(val)
		if err != nil {
			fmt.Fprint(os.Stderr, "gogram: MP4 tag ver is not a number!!!")
		}
		f.version = uint8(ver)
	}
	f.nVersion = math.MaxUint8
	if val, contained := tagMap["nver"]; contained {
		ver, err := strconv.Atoi(val)
		if err != nil {
			fmt.Fprint(os.Stderr, "gogram: MP4 tag nver is not a number!!!")
		}
		f.nVersion = uint8(ver)
	}
	if val, contained := tagMap["size"]; contained {
		if val == "dynamic" {
			f.set(fieldSizeDynamic)
		} else {
			size, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				fmt.Fprint(os.Stderr, "gogram: MP4 tag size is not a number!!!")
			}
			f.size = uint(size)
		}
	}
	f.length = LengthUnlimited
	if val, contained := tagMap["len"]; contained {
		if val == "dynamic" {
			f.set(fieldLengthDynamic)
		} else {
			l, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				fmt.Fprint(os.Stderr, "gogram: MP4 tag len is not a number!!!")
			}
			f.length = uint(l)
		}
	}
	return f
}

func parseFieldTag(str string) map[string]string {
	tag := make(map[string]string, 8)
	list := strings.Split(str, ",")
	for _, e := range list {
		kv := strings.SplitN(e, "=", 2)
		if len(kv) == 2 {
			tag[strings.Trim(kv[0], " ")] = strings.Trim(kv[1], " ")
		} else {
			tag[strings.Trim(kv[0], " ")] = ""
		}
	}

	return tag
}

type fieldInstance struct {
	field
	cfo ICustomFieldObject
}

func resolveFieldInstance(f *field, box IImmutableBox, parent reflect.Value, ctx Context) *fieldInstance {
	fi := fieldInstance{
		field: *f,
	}

	cfo, ok := parent.Addr().Interface().(ICustomFieldObject)
	if ok {
		fi.cfo = cfo
	} else {
		fi.cfo = box
	}

	if fi.is(fieldSizeDynamic) {
		fi.size = fi.cfo.GetFieldSize(f.name, ctx)
	}

	if fi.is(fieldLengthDynamic) {
		fi.length = fi.cfo.GetFieldLength(f.name, ctx)
	}

	return &fi
}

func isTargetField(box IImmutableBox, fi *fieldInstance, ctx Context) bool {
	if box.GetVersion() != math.MaxUint8 {
		if fi.version != math.MaxUint8 && box.GetVersion() != fi.version {
			return false
		}

		if fi.nVersion != math.MaxUint8 && box.GetVersion() == fi.nVersion {
			return false
		}
	}
	if fi.optFlag != 0 && box.GetFlags()&fi.optFlag == 0 {
		return false
	}
	if fi.nOptFlag != 0 && box.GetFlags()&fi.nOptFlag != 0 {
		return false
	}
	if fi.is(fieldOptDynamic) && !fi.cfo.IsOptFieldEnabled(fi.name, ctx) {
		return false
	}
	return true
}

type ICustomFieldObject interface {
	GetFieldSize(name string, ctx Context) uint
	GetFieldLength(name string, ctx Context) uint
	IsOptFieldEnabled(name string, ctx Context) bool
	BeforeUnmarshal(r io.ReadSeeker, size uint64, ctx Context) (n uint64, override bool, err error)
	OnReadField(name string, r ReadSeeker, leftBits uint64, ctx Context) (rbits uint64, override bool, err error)
}

type IImmutableBox interface {
	ICustomFieldObject
	GetVersion() uint8
	GetFlags() uint32
	CheckFlag(uint32) bool
	GetType() BoxType
}

// IBox is common interface of box
type IBox interface {
	IImmutableBox
	SetVersion(uint8)
	SetFlags(uint32)
	AddFlag(uint32)
	RemoveFlag(uint32)
}

type Meta struct {
	FullBox `mp4:"0,extend"`
}

func BoxTypeMeta() BoxType { return StrToBoxType("meta") }

// GetType returns the BoxType
func (*Meta) GetType() BoxType {
	return BoxTypeMeta()
}

func (meta *Meta) BeforeUnmarshal(r io.ReadSeeker, size uint64, ctx Context) (n uint64, override bool, err error) {
	// for Apple Quick Time
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, false, err
	}
	if _, err := r.Seek(-int64(len(buf)), io.SeekCurrent); err != nil {
		return 0, false, err
	}
	if buf[0]|buf[1]|buf[2]|buf[3] != 0x00 {
		meta.Version = 0
		meta.Flags = [3]byte{0, 0, 0}
		return 0, true, nil
	}
	return 0, false, nil
}

// -------------------------- ReadSeeker --------------------------

type Reader interface {
	io.Reader

	// alignment:
	//  |-1-byte-block-|--------------|--------------|--------------|
	//  |<-offset->|<-------------------width---------------------->|
	ReadBits(width uint) (data []byte, err error)

	ReadBit() (bit bool, err error)
}

type ReadSeeker interface {
	Reader
	io.Seeker
}

type reader struct {
	reader io.Reader
	octet  byte
	width  uint
}

func NewReader(r io.Reader) Reader {
	return &reader{reader: r}
}

func (r *reader) Read(p []byte) (n int, err error) {
	if r.width != 0 {
		return 0, fmt.Errorf("bitio: reader is not aligned")
	}
	return r.reader.Read(p)
}

func (r *reader) ReadBits(size uint) ([]byte, error) {
	bytes := (size + 7) / 8
	data := make([]byte, bytes)
	offset := (bytes * 8) - (size)

	for i := uint(0); i < size; i++ {
		bit, err := r.ReadBit()
		if err != nil {
			return nil, err
		}

		byteIdx := (offset + i) / 8
		bitIdx := 7 - (offset+i)%8
		if bit {
			data[byteIdx] |= 0x1 << bitIdx
		}
	}

	return data, nil
}

func (r *reader) ReadBit() (bool, error) {
	if r.width == 0 {
		buf := make([]byte, 1)
		if n, err := r.reader.Read(buf); err != nil {
			return false, err
		} else if n != 1 {
			return false, io.EOF
		}
		r.octet = buf[0]
		r.width = 8
	}

	r.width--
	return (r.octet>>r.width)&0x01 != 0, nil
}

type readSeeker struct {
	reader
	seeker io.Seeker
}

func NewReadSeeker(r io.ReadSeeker) ReadSeeker {
	return &readSeeker{
		reader: reader{reader: r},
		seeker: r,
	}
}

func (r *readSeeker) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent && r.reader.width != 0 {
		return 0, fmt.Errorf("bitio: reader is not aligned")
	}
	n, err := r.seeker.Seek(offset, whence)
	if err != nil {
		return n, err
	}
	r.reader.width = 0
	return n, nil
}
