// Copyright (c) 2025 RoseLoverX

package utils

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

type QRCode struct {
	Content string
	Level   RecoveryLevel

	VersionNumber int

	ForegroundColor color.Color
	BackgroundColor color.Color

	DisableBorder bool

	version qrCodeVersion

	data   *Bitset
	symbol *symbol
	mask   int
}

func NewQRCode(content string) (*QRCode, error) {
	encoder := newDataEncoder()
	encoded, err := encoder.encode([]byte(content))
	if err != nil {
		return nil, err
	}

	q := &QRCode{
		Content: content,
		Level:   Medium,

		VersionNumber:   version5.version,
		ForegroundColor: color.Black,
		BackgroundColor: color.White,

		data:    encoded,
		version: version5,
	}

	return q, nil
}

func (q *QRCode) Bitmap() ([][]bool, error) {
	if err := q.encode(); err != nil {
		return nil, err
	}
	return q.symbol.bitmap(), nil
}

func (q *QRCode) Image(size int) (image.Image, error) {
	if err := q.encode(); err != nil {
		return nil, err
	}
	realSize := q.symbol.size
	if size < 0 {
		size = -size * realSize
	}
	if size < realSize {
		size = realSize
	}
	rect := image.Rectangle{Min: image.Point{0, 0}, Max: image.Point{size, size}}
	palette := color.Palette([]color.Color{q.BackgroundColor, q.ForegroundColor})
	img := image.NewPaletted(rect, palette)
	fgIdx := uint8(img.Palette.Index(q.ForegroundColor))
	bitmap := q.symbol.bitmap()
	modulesPerPixel := float64(realSize) / float64(size)
	for y := 0; y < size; y++ {
		y2 := int(float64(y) * modulesPerPixel)
		for x := 0; x < size; x++ {
			x2 := int(float64(x) * modulesPerPixel)
			if bitmap[y2][x2] {
				img.Pix[img.PixOffset(x, y)] = fgIdx
			}
		}
	}
	return img, nil
}

func (q *QRCode) PNG(size int) ([]byte, error) {
	img, err := q.Image(size)
	if err != nil {
		return nil, err
	}
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	var b bytes.Buffer
	if err := encoder.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (q *QRCode) Base64PNG(size int) string {
	pngData, err := q.PNG(size)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(pngData)
}

func (q *QRCode) encode() error {
	if q.symbol != nil {
		return nil
	}
	numTerminatorBits := q.version.numTerminatorBitsRequired(q.data.Len())
	q.addTerminatorBits(numTerminatorBits)
	if err := q.addPadding(); err != nil {
		return err
	}
	encoded, err := q.encodeBlocks()
	if err != nil {
		return err
	}
	const numMasks = 8
	bestMask := 0
	bestPenalty := -1
	for mask := range numMasks {
		symbol, err := buildRegularSymbol(q.version, mask, encoded, !q.DisableBorder)
		if err != nil {
			return fmt.Errorf("failed to build QR symbol: %w", err)
		}
		penalty := symbol.penaltyScore()
		if bestPenalty == -1 || penalty < bestPenalty {
			bestPenalty = penalty
			bestMask = mask
			q.symbol = symbol
		}
	}
	q.mask = bestMask
	return nil
}

func (q *QRCode) addTerminatorBits(num int) {
	q.data.AppendNumBools(num, false)
}

func (q *QRCode) addPadding() error {
	numDataBits := q.version.numDataBits()
	if q.data.Len() == numDataBits {
		return nil
	}
	q.data.AppendNumBools(q.version.numBitsToPadToCodeword(q.data.Len()), false)
	padding := [2]*Bitset{
		NewBitset(true, true, true, false, true, true, false, false),
		NewBitset(false, false, false, true, false, false, false, true),
	}
	i := 0
	for numDataBits-q.data.Len() >= 8 {
		if err := q.data.Append(padding[i]); err != nil {
			return err
		}
		i = 1 - i
	}
	if q.data.Len() != numDataBits {
		return fmt.Errorf("padding bug: got %d bits, expected %d", q.data.Len(), numDataBits)
	}
	return nil
}

func (q *QRCode) encodeBlocks() (*Bitset, error) {
	type dataBlock struct {
		data          *Bitset
		ecStartOffset int
	}

	block := make([]dataBlock, q.version.numBlocks())

	start := 0
	end := 0
	blockID := 0

	for _, b := range q.version.block {
		for j := 0; j < b.numBlocks; j++ {
			start = end
			end = start + b.numDataCodewords*8

			numErrorCodewords := b.numCodewords - b.numDataCodewords
			chunk, err := q.data.Substr(start, end)
			if err != nil {
				return nil, err
			}
			encoded, err := reedSolomonEncode(chunk, numErrorCodewords)
			if err != nil {
				return nil, err
			}
			block[blockID].data = encoded
			block[blockID].ecStartOffset = end - start

			blockID++
		}
	}

	result := NewBitset()

	working := true
	for i := 0; working; i += 8 {
		working = false
		for j := range block {
			if i >= block[j].ecStartOffset {
				continue
			}
			snippet, err := block[j].data.Substr(i, i+8)
			if err != nil {
				return nil, err
			}
			if err := result.Append(snippet); err != nil {
				return nil, err
			}
			working = true
		}
	}

	working = true
	for i := 0; working; i += 8 {
		working = false
		for j := range block {
			offset := i + block[j].ecStartOffset
			if offset >= block[j].data.Len() {
				continue
			}
			snippet, err := block[j].data.Substr(offset, offset+8)
			if err != nil {
				return nil, err
			}
			if err := result.Append(snippet); err != nil {
				return nil, err
			}
			working = true
		}
	}

	result.AppendNumBools(q.version.numRemainderBits, false)
	return result, nil
}

func (q *QRCode) ToSmallString(inverse bool) string {
	bits, err := q.Bitmap()
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	for y := 0; y < len(bits)-1; y += 2 {
		for x := range bits[y] {
			top := bits[y][x]
			bottom := bits[y+1][x]
			switch {
			case top == bottom && top != inverse:
				buf.WriteString(" ")
			case top == bottom && top == inverse:
				buf.WriteString("█")
			case top != inverse:
				buf.WriteString("▄")
			default:
				buf.WriteString("▀")
			}
		}
		buf.WriteString("\n")
	}
	if len(bits)%2 == 1 {
		y := len(bits) - 1
		for x := range bits[y] {
			if bits[y][x] != inverse {
				buf.WriteString(" ")
			} else {
				buf.WriteString("▀")
			}
		}
		buf.WriteString("\n")
	}
	return buf.String()
}

type dataEncoder struct {
	charCountBits int
	modeIndicator *Bitset
}

func newDataEncoder() *dataEncoder {
	return &dataEncoder{charCountBits: 8, modeIndicator: NewBitset(b0, b1, b0, b0)}
}

func (d *dataEncoder) encode(data []byte) (*Bitset, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to encode")
	}
	if len(data) > (1<<uint(d.charCountBits))-1 {
		return nil, fmt.Errorf("segment too long")
	}
	encoded := NewBitset()
	if err := encoded.Append(d.modeIndicator); err != nil {
		return nil, err
	}
	if err := encoded.AppendUint32(uint32(len(data)), d.charCountBits); err != nil {
		return nil, err
	}
	for _, b := range data {
		if err := encoded.AppendByte(b, 8); err != nil {
			return nil, err
		}
	}
	return encoded, nil
}

const (
	b0 = false
	b1 = true
)

type Bitset struct {
	numBits int
	bits    []byte
}

func NewBitset(v ...bool) *Bitset {
	b := &Bitset{bits: make([]byte, 0)}
	b.AppendBools(v...)
	return b
}

func CloneBitset(from *Bitset) *Bitset {
	dup := make([]byte, len(from.bits))
	copy(dup, from.bits)
	return &Bitset{numBits: from.numBits, bits: dup}
}

func (b *Bitset) Substr(start, end int) (*Bitset, error) {
	if start > end || end > b.numBits {
		return nil, fmt.Errorf("substr out of range start=%d end=%d len=%d", start, end, b.numBits)
	}
	res := NewBitset()
	res.ensureCapacity(end - start)
	for i := start; i < end; i++ {
		if b.At(i) {
			res.bits[res.numBits/8] |= 0x80 >> uint(res.numBits%8)
		}
		res.numBits++
	}
	return res, nil
}

func (b *Bitset) Append(other *Bitset) error {
	b.ensureCapacity(other.numBits)
	for i := 0; i < other.numBits; i++ {
		if other.At(i) {
			b.bits[b.numBits/8] |= 0x80 >> uint(b.numBits%8)
		}
		b.numBits++
	}
	return nil
}

func (b *Bitset) AppendBools(bits ...bool) {
	b.ensureCapacity(len(bits))
	for _, v := range bits {
		if v {
			b.bits[b.numBits/8] |= 0x80 >> uint(b.numBits%8)
		}
		b.numBits++
	}
}

func (b *Bitset) AppendNumBools(num int, value bool) {
	for i := 0; i < num; i++ {
		b.AppendBools(value)
	}
}

func (b *Bitset) AppendBytes(data []byte) error {
	for _, v := range data {
		if err := b.AppendByte(v, 8); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bitset) AppendByte(value byte, numBits int) error {
	b.ensureCapacity(numBits)
	if numBits > 8 {
		return fmt.Errorf("numBits %d out of range", numBits)
	}
	for i := numBits - 1; i >= 0; i-- {
		if value&(1<<uint(i)) != 0 {
			b.bits[b.numBits/8] |= 0x80 >> uint(b.numBits%8)
		}
		b.numBits++
	}
	return nil
}

func (b *Bitset) AppendUint32(value uint32, numBits int) error {
	b.ensureCapacity(numBits)
	if numBits > 32 {
		return fmt.Errorf("numBits %d out of range", numBits)
	}
	for i := numBits - 1; i >= 0; i-- {
		if value&(1<<uint(i)) != 0 {
			b.bits[b.numBits/8] |= 0x80 >> uint(b.numBits%8)
		}
		b.numBits++
	}
	return nil
}

func (b *Bitset) ensureCapacity(numBits int) {
	needed := b.numBits + numBits
	bytesNeeded := needed / 8
	if needed%8 != 0 {
		bytesNeeded++
	}
	if len(b.bits) >= bytesNeeded {
		return
	}
	extra := bytesNeeded + 2*len(b.bits)
	b.bits = append(b.bits, make([]byte, extra-len(b.bits))...)
}

func (b *Bitset) Len() int {
	return b.numBits
}

func (b *Bitset) At(index int) bool {
	if index < 0 || index >= b.numBits {
		return false
	}
	return (b.bits[index/8] & (0x80 >> byte(index%8))) != 0
}

func (b *Bitset) ByteAt(index int) byte {
	if index < 0 || index >= b.numBits {
		return 0
	}
	var result byte
	for i := index; i < index+8 && i < b.numBits; i++ {
		result <<= 1
		if b.At(i) {
			result |= 1
		}
	}
	return result
}

type symbol struct {
	module        [][]bool
	isUsed        [][]bool
	size          int
	symbolSize    int
	quietZoneSize int
}

func newSymbol(size int, quietZoneSize int) *symbol {
	m := &symbol{}
	m.module = make([][]bool, size+2*quietZoneSize)
	m.isUsed = make([][]bool, size+2*quietZoneSize)
	for i := range m.module {
		m.module[i] = make([]bool, size+2*quietZoneSize)
		m.isUsed[i] = make([]bool, size+2*quietZoneSize)
	}
	m.size = size + 2*quietZoneSize
	m.symbolSize = size
	m.quietZoneSize = quietZoneSize
	return m
}

func (m *symbol) get(x, y int) bool {
	return m.module[y+m.quietZoneSize][x+m.quietZoneSize]
}

func (m *symbol) empty(x, y int) bool {
	return !m.isUsed[y+m.quietZoneSize][x+m.quietZoneSize]
}

func (m *symbol) set(x, y int, v bool) {
	m.module[y+m.quietZoneSize][x+m.quietZoneSize] = v
	m.isUsed[y+m.quietZoneSize][x+m.quietZoneSize] = true
}

func (m *symbol) set2dPattern(x, y int, pattern [][]bool) {
	for j, row := range pattern {
		for i, v := range row {
			m.set(x+i, y+j, v)
		}
	}
}

func (m *symbol) bitmap() [][]bool {
	dup := make([][]bool, len(m.module))
	for i := range m.module {
		dup[i] = append([]bool(nil), m.module[i]...)
	}
	return dup
}

const (
	penaltyWeight1 = 3
	penaltyWeight2 = 3
	penaltyWeight3 = 40
	penaltyWeight4 = 10
)

func (m *symbol) penaltyScore() int {
	return m.penalty1() + m.penalty2() + m.penalty3() + m.penalty4()
}

func (m *symbol) penalty1() int {
	penalty := 0
	for x := 0; x < m.symbolSize; x++ {
		last := m.get(x, 0)
		count := 1
		for y := 1; y < m.symbolSize; y++ {
			v := m.get(x, y)
			if v != last {
				count = 1
				last = v
			} else {
				count++
				if count == 6 {
					penalty += penaltyWeight1 + 1
				} else if count > 6 {
					penalty++
				}
			}
		}
	}
	for y := 0; y < m.symbolSize; y++ {
		last := m.get(0, y)
		count := 1
		for x := 1; x < m.symbolSize; x++ {
			v := m.get(x, y)
			if v != last {
				count = 1
				last = v
			} else {
				count++
				if count == 6 {
					penalty += penaltyWeight1 + 1
				} else if count > 6 {
					penalty++
				}
			}
		}
	}
	return penalty
}

func (m *symbol) penalty2() int {
	penalty := 0
	for y := 1; y < m.symbolSize; y++ {
		for x := 1; x < m.symbolSize; x++ {
			topLeft := m.get(x-1, y-1)
			if topLeft == m.get(x, y-1) && topLeft == m.get(x-1, y) && topLeft == m.get(x, y) {
				penalty++
			}
		}
	}
	return penalty * penaltyWeight2
}

func (m *symbol) penalty3() int {
	penalty := 0
	for y := 0; y < m.symbolSize; y++ {
		var buffer int16
		for x := 0; x < m.symbolSize; x++ {
			buffer <<= 1
			if m.get(x, y) {
				buffer |= 1
			}
			switch buffer & 0x7ff {
			case 0x05d, 0x5d0:
				penalty += penaltyWeight3
				buffer = 0xff
			default:
				if x == m.symbolSize-1 && (buffer&0x7f) == 0x5d {
					penalty += penaltyWeight3
				}
			}
		}
	}
	for x := 0; x < m.symbolSize; x++ {
		var buffer int16
		for y := 0; y < m.symbolSize; y++ {
			buffer <<= 1
			if m.get(x, y) {
				buffer |= 1
			}
			switch buffer & 0x7ff {
			case 0x05d, 0x5d0:
				penalty += penaltyWeight3
				buffer = 0xff
			default:
				if y == m.symbolSize-1 && (buffer&0x7f) == 0x5d {
					penalty += penaltyWeight3
				}
			}
		}
	}
	return penalty
}

func (m *symbol) penalty4() int {
	numModules := m.symbolSize * m.symbolSize
	dark := 0
	for x := 0; x < m.symbolSize; x++ {
		for y := 0; y < m.symbolSize; y++ {
			if m.get(x, y) {
				dark++
			}
		}
	}
	deviation := numModules/2 - dark
	if deviation < 0 {
		deviation = -deviation
	}
	return penaltyWeight4 * (deviation / (numModules / 20))
}

type regularSymbol struct {
	version qrCodeVersion
	mask    int
	data    *Bitset
	symbol  *symbol
	size    int
}

var alignmentPatternCenter = [][]int{
	{}, {},
	{6, 18}, {6, 22},
	{6, 26}, {6, 30},
}

var finderPattern = [][]bool{
	{b1, b1, b1, b1, b1, b1, b1},
	{b1, b0, b0, b0, b0, b0, b1},
	{b1, b0, b1, b1, b1, b0, b1},
	{b1, b0, b1, b1, b1, b0, b1},
	{b1, b0, b1, b1, b1, b0, b1},
	{b1, b0, b0, b0, b0, b0, b1},
	{b1, b1, b1, b1, b1, b1, b1},
}

var finderPatternHorizontalBorder = [][]bool{{b0, b0, b0, b0, b0, b0, b0, b0}}
var finderPatternVerticalBorder = [][]bool{{b0}, {b0}, {b0}, {b0}, {b0}, {b0}, {b0}, {b0}}

var alignmentPattern = [][]bool{
	{b1, b1, b1, b1, b1},
	{b1, b0, b0, b0, b1},
	{b1, b0, b1, b0, b1},
	{b1, b0, b0, b0, b1},
	{b1, b1, b1, b1, b1},
}

func buildRegularSymbol(version qrCodeVersion, mask int, data *Bitset, includeQuietZone bool) (*symbol, error) {
	quietZone := 0
	if includeQuietZone {
		quietZone = 4
	}
	m := &regularSymbol{
		version: version,
		mask:    mask,
		data:    data,
		symbol:  newSymbol(version.symbolSize(), quietZone),
		size:    version.symbolSize(),
	}
	m.addFinderPatterns()
	m.addAlignmentPatterns()
	m.addTimingPatterns()
	if err := m.addFormatInfo(); err != nil {
		return nil, err
	}
	m.addVersionInfo()
	if ok, err := m.addData(); !ok {
		return nil, err
	}
	return m.symbol, nil
}

func (m *regularSymbol) addFinderPatterns() {
	fpSize := len(finderPattern)
	m.symbol.set2dPattern(0, 0, finderPattern)
	m.symbol.set2dPattern(0, fpSize, finderPatternHorizontalBorder)
	m.symbol.set2dPattern(fpSize, 0, finderPatternVerticalBorder)
	m.symbol.set2dPattern(m.size-fpSize, 0, finderPattern)
	m.symbol.set2dPattern(m.size-fpSize-1, fpSize, finderPatternHorizontalBorder)
	m.symbol.set2dPattern(m.size-fpSize-1, 0, finderPatternVerticalBorder)
	m.symbol.set2dPattern(0, m.size-fpSize, finderPattern)
	m.symbol.set2dPattern(0, m.size-fpSize-1, finderPatternHorizontalBorder)
	m.symbol.set2dPattern(fpSize, m.size-fpSize-1, finderPatternVerticalBorder)
}

func (m *regularSymbol) addAlignmentPatterns() {
	centers := alignmentPatternCenter[m.version.version]
	for _, x := range centers {
		for _, y := range centers {
			if !m.symbol.empty(x, y) {
				continue
			}
			m.symbol.set2dPattern(x-2, y-2, alignmentPattern)
		}
	}
}

func (m *regularSymbol) addTimingPatterns() {
	value := true
	for i := len(finderPattern) + 1; i < m.size-len(finderPattern); i++ {
		m.symbol.set(i, len(finderPattern)-1, value)
		m.symbol.set(len(finderPattern)-1, i, value)
		value = !value
	}
}

func (m *regularSymbol) addFormatInfo() error {
	f, err := m.version.formatInfo(m.mask)
	if err != nil {
		return err
	}
	fpSize := len(finderPattern)
	l := f.Len() - 1
	for i := 0; i <= 7; i++ {
		m.symbol.set(m.size-i-1, fpSize+1, f.At(l-i))
	}
	for i := 0; i <= 5; i++ {
		m.symbol.set(fpSize+1, i, f.At(l-i))
	}
	m.symbol.set(fpSize+1, fpSize, f.At(l-6))
	m.symbol.set(fpSize+1, fpSize+1, f.At(l-7))
	m.symbol.set(fpSize, fpSize+1, f.At(l-8))
	for i := 9; i <= 14; i++ {
		m.symbol.set(14-i, fpSize+1, f.At(l-i))
	}
	for i := 8; i <= 14; i++ {
		m.symbol.set(fpSize+1, m.size-fpSize+i-8, f.At(l-i))
	}
	m.symbol.set(fpSize+1, m.size-fpSize-1, true)
	return nil
}

func (m *regularSymbol) addVersionInfo() {
	fpSize := len(finderPattern)
	v := m.version.versionInfo()
	if v == nil {
		return
	}
	l := v.Len() - 1
	for i := 0; i < v.Len(); i++ {
		m.symbol.set(i/3, m.size-fpSize-4+i%3, v.At(l-i))
		m.symbol.set(m.size-fpSize-4+i%3, i/3, v.At(l-i))
	}
}

func (m *regularSymbol) addData() (bool, error) {
	xOffset := 1
	dirUp := true
	x := m.size - 2
	y := m.size - 1
	for i := 0; i < m.data.Len(); i++ {
		mask := false
		switch m.mask {
		case 0:
			mask = (y+x+xOffset)%2 == 0
		case 1:
			mask = y%2 == 0
		case 2:
			mask = (x+xOffset)%3 == 0
		case 3:
			mask = (y+x+xOffset)%3 == 0
		case 4:
			mask = (y/2+(x+xOffset)/3)%2 == 0
		case 5:
			mask = (y*(x+xOffset))%2+(y*(x+xOffset))%3 == 0
		case 6:
			mask = ((y*(x+xOffset))%2+((y*(x+xOffset))%3))%2 == 0
		case 7:
			mask = ((y+x+xOffset)%2+((y*(x+xOffset))%3))%2 == 0
		}
		m.symbol.set(x+xOffset, y, mask != m.data.At(i))
		if i == m.data.Len()-1 {
			break
		}
		for {
			if xOffset == 1 {
				xOffset = 0
			} else {
				xOffset = 1
				if dirUp {
					if y > 0 {
						y--
					} else {
						dirUp = false
						x -= 2
					}
				} else {
					if y < m.size-1 {
						y++
					} else {
						dirUp = true
						x -= 2
					}
				}
			}
			if x == 5 {
				x--
			}
			if m.symbol.empty(x+xOffset, y) {
				break
			}
		}
	}
	return true, nil
}

type gfElement uint8

const (
	gfZero = gfElement(0)
	gfOne  = gfElement(1)
)

var (
	gfExpTable = [256]gfElement{
		1, 2, 4, 8, 16, 32, 64, 128, 29, 58,
		116, 232, 205, 135, 19, 38, 76, 152, 45, 90,
		180, 117, 234, 201, 143, 3, 6, 12, 24, 48,
		96, 192, 157, 39, 78, 156, 37, 74, 148, 53,
		106, 212, 181, 119, 238, 193, 159, 35, 70, 140,
		5, 10, 20, 40, 80, 160, 93, 186, 105, 210,
		185, 111, 222, 161, 95, 190, 97, 194, 153, 47,
		94, 188, 101, 202, 137, 15, 30, 60, 120, 240,
		253, 231, 211, 187, 107, 214, 177, 127, 254, 225,
		223, 163, 91, 182, 113, 226, 217, 175, 67, 134,
		17, 34, 68, 136, 13, 26, 52, 104, 208, 189,
		103, 206, 129, 31, 62, 124, 248, 237, 199, 147,
		59, 118, 236, 197, 151, 51, 102, 204, 133, 23,
		46, 92, 184, 109, 218, 169, 79, 158, 33, 66,
		132, 21, 42, 84, 168, 77, 154, 41, 82, 164,
		85, 170, 73, 146, 57, 114, 228, 213, 183, 115,
		230, 209, 191, 99, 198, 145, 63, 126, 252, 229,
		215, 179, 123, 246, 241, 255, 227, 219, 171, 75,
		150, 49, 98, 196, 149, 55, 110, 220, 165, 87,
		174, 65, 130, 25, 50, 100, 200, 141, 7, 14,
		28, 56, 112, 224, 221, 167, 83, 166, 81, 162,
		89, 178, 121, 242, 249, 239, 195, 155, 43, 86,
		172, 69, 138, 9, 18, 36, 72, 144, 61, 122,
		244, 245, 247, 243, 251, 235, 203, 139, 11, 22,
		44, 88, 176, 125, 250, 233, 207, 131, 27, 54,
		108, 216, 173, 71, 142, 1,
	}

	gfLogTable = [256]int{
		-1, 0, 1, 25, 2, 50, 26, 198, 3, 223,
		51, 238, 27, 104, 199, 75, 4, 100, 224, 14,
		52, 141, 239, 129, 28, 193, 105, 248, 200, 8,
		76, 113, 5, 138, 101, 47, 225, 36, 15, 33,
		53, 147, 142, 218, 240, 18, 130, 69, 29, 181,
		194, 125, 106, 39, 249, 185, 201, 154, 9, 120,
		77, 228, 114, 166, 6, 191, 139, 98, 102, 221,
		48, 253, 226, 152, 37, 179, 16, 145, 34, 136,
		54, 208, 148, 206, 143, 150, 219, 189, 241, 210,
		19, 92, 131, 56, 70, 64, 30, 66, 182, 163,
		195, 72, 126, 110, 107, 58, 40, 84, 250, 133,
		186, 61, 202, 94, 155, 159, 10, 21, 121, 43,
		78, 212, 229, 172, 115, 243, 167, 87, 7, 112,
		192, 247, 140, 128, 99, 13, 103, 74, 222, 237,
		49, 197, 254, 24, 227, 165, 153, 119, 38, 184,
		180, 124, 17, 68, 146, 217, 35, 32, 137, 46,
		55, 63, 209, 91, 149, 188, 207, 205, 144, 135,
		151, 178, 220, 252, 190, 97, 242, 86, 211, 171,
		20, 42, 93, 158, 132, 60, 57, 83, 71, 109,
		65, 162, 31, 45, 67, 216, 183, 123, 164, 118,
		196, 23, 73, 236, 127, 12, 111, 246, 108, 161,
		59, 82, 41, 157, 85, 170, 251, 96, 134, 177,
		187, 204, 62, 90, 203, 89, 95, 176, 156, 169,
		160, 81, 11, 245, 22, 235, 122, 117, 44, 215,
		79, 174, 213, 233, 230, 231, 173, 232, 116, 214,
		244, 234, 168, 80, 88, 175,
	}
)

func gfAdd(a, b gfElement) gfElement {
	return a ^ b
}

func gfMultiply(a, b gfElement) gfElement {
	if a == gfZero || b == gfZero {
		return gfZero
	}
	return gfExpTable[(gfLogTable[a]+gfLogTable[b])%255]
}

func gfDivide(a, b gfElement) (gfElement, error) {
	if b == gfZero {
		return 0, fmt.Errorf("gf divide by zero")
	}
	if a == gfZero {
		return gfZero, nil
	}
	inv, err := gfInverse(b)
	if err != nil {
		return 0, err
	}
	return gfMultiply(a, inv), nil
}

func gfInverse(a gfElement) (gfElement, error) {
	if a == gfZero {
		return 0, fmt.Errorf("gf inverse of zero")
	}
	return gfExpTable[255-gfLogTable[a]], nil
}

type gfPoly struct {
	term []gfElement
}

func newGFPolyFromData(data *Bitset) gfPoly {
	numTotalBytes := data.Len() / 8
	if data.Len()%8 != 0 {
		numTotalBytes++
	}
	result := gfPoly{term: make([]gfElement, numTotalBytes)}
	i := numTotalBytes - 1
	for j := 0; j < data.Len(); j += 8 {
		result.term[i] = gfElement(data.ByteAt(j))
		i--
	}
	return result
}

func newGFPolyMonomial(term gfElement, degree int) gfPoly {
	if term == gfZero {
		return gfPoly{}
	}
	result := gfPoly{term: make([]gfElement, degree+1)}
	result.term[degree] = term
	return result
}

func (e gfPoly) data(numTerms int) []byte {
	result := make([]byte, numTerms)
	i := numTerms - len(e.term)
	for j := len(e.term) - 1; j >= 0; j-- {
		result[i] = byte(e.term[j])
		i++
	}
	return result
}

func (e gfPoly) numTerms() int {
	return len(e.term)
}

func gfPolyMultiply(a, b gfPoly) gfPoly {
	numATerms := a.numTerms()
	numBTerms := b.numTerms()
	result := gfPoly{term: make([]gfElement, numATerms+numBTerms)}
	for i := 0; i < numATerms; i++ {
		for j := 0; j < numBTerms; j++ {
			if a.term[i] != 0 && b.term[j] != 0 {
				monomial := gfPoly{term: make([]gfElement, i+j+1)}
				monomial.term[i+j] = gfMultiply(a.term[i], b.term[j])
				result = gfPolyAdd(result, monomial)
			}
		}
	}
	return result.normalised()
}

func gfPolyRemainder(numerator, denominator gfPoly) (gfPoly, error) {
	if denominator.equals(gfPoly{}) {
		return gfPoly{}, fmt.Errorf("gfPolyRemainder divide by zero")
	}
	remainder := numerator
	for remainder.numTerms() >= denominator.numTerms() {
		degree := remainder.numTerms() - denominator.numTerms()
		coef, err := gfDivide(
			remainder.term[remainder.numTerms()-1],
			denominator.term[denominator.numTerms()-1],
		)
		if err != nil {
			return gfPoly{}, err
		}
		divisor := gfPolyMultiply(denominator, newGFPolyMonomial(coef, degree))
		remainder = gfPolyAdd(remainder, divisor)
	}
	return remainder.normalised(), nil
}

func gfPolyAdd(a, b gfPoly) gfPoly {
	numATerms := a.numTerms()
	numBTerms := b.numTerms()
	numTerms := numATerms
	if numBTerms > numTerms {
		numTerms = numBTerms
	}
	result := gfPoly{term: make([]gfElement, numTerms)}
	for i := 0; i < numTerms; i++ {
		switch {
		case numATerms > i && numBTerms > i:
			result.term[i] = gfAdd(a.term[i], b.term[i])
		case numATerms > i:
			result.term[i] = a.term[i]
		default:
			result.term[i] = b.term[i]
		}
	}
	return result.normalised()
}

func (e gfPoly) normalised() gfPoly {
	numTerms := e.numTerms()
	maxNonzeroTerm := numTerms - 1
	for i := numTerms - 1; i >= 0; i-- {
		if e.term[i] != 0 {
			break
		}
		maxNonzeroTerm = i - 1
	}
	if maxNonzeroTerm < 0 {
		return gfPoly{}
	}
	if maxNonzeroTerm < numTerms-1 {
		e.term = e.term[0 : maxNonzeroTerm+1]
	}
	return e
}

func (e gfPoly) equals(other gfPoly) bool {
	var minPoly *gfPoly
	var maxPoly *gfPoly
	if e.numTerms() > other.numTerms() {
		minPoly = &other
		maxPoly = &e
	} else {
		minPoly = &e
		maxPoly = &other
	}
	numMinTerms := minPoly.numTerms()
	numMaxTerms := maxPoly.numTerms()
	for i := range numMinTerms {
		if e.term[i] != other.term[i] {
			return false
		}
	}
	for i := numMinTerms; i < numMaxTerms; i++ {
		if maxPoly.term[i] != 0 {
			return false
		}
	}
	return true
}

func reedSolomonEncode(data *Bitset, numECBytes int) (*Bitset, error) {
	ecPoly := newGFPolyFromData(data)
	ecPoly = gfPolyMultiply(ecPoly, newGFPolyMonomial(gfOne, numECBytes))
	generator, err := rsGeneratorPoly(numECBytes)
	if err != nil {
		return nil, err
	}
	remainder, err := gfPolyRemainder(ecPoly, generator)
	if err != nil {
		return nil, err
	}
	result := CloneBitset(data)
	if err := result.AppendBytes(remainder.data(numECBytes)); err != nil {
		return nil, err
	}
	return result, nil
}

func rsGeneratorPoly(degree int) (gfPoly, error) {
	if degree < 2 {
		return gfPoly{}, fmt.Errorf("generator degree %d < 2", degree)
	}
	generator := gfPoly{term: []gfElement{1}}
	for i := range degree {
		nextPoly := gfPoly{term: []gfElement{gfExpTable[i], 1}}
		generator = gfPolyMultiply(generator, nextPoly)
	}
	return generator, nil
}

type RecoveryLevel int

const (
	Medium RecoveryLevel = 0
)

type qrCodeVersion struct {
	version          int
	level            RecoveryLevel
	block            []block
	numRemainderBits int
}

type block struct{ numBlocks, numCodewords, numDataCodewords int }

var version5 = qrCodeVersion{version: 5, level: Medium, block: []block{{2, 67, 43}}, numRemainderBits: 7}

var formatBitSequence = []uint32{
	0x5412, 0x4172, 0x5e7c, 0x4b1c, 0x55ae, 0x5099,
	0x5fc0, 0x5af7, 0x6793, 0x62a4, 0x6dfd, 0x68ca,
	0x7678, 0x734f, 0x7c16, 0x7921, 0x06de, 0x03e9,
	0x0cb0, 0x0987, 0x1735, 0x1202, 0x1d5b, 0x186c,
	0x2508, 0x203f, 0x2f66, 0x2a51, 0x34e3, 0x31d4,
	0x3e8d, 0x3bba,
}

var versionBitSequence = []uint32{
	0x00000, 0x00000, 0x00000, 0x00000, 0x00000, 0x00000,
	0x00000, 0x07c94, 0x085bc, 0x09a99, 0x0a4d3, 0x0bbf6,
	0x0c762, 0x0d847, 0x0e60d, 0x0f928, 0x10b78, 0x1145d,
	0x12a17, 0x13532, 0x149a6, 0x15683, 0x168c9, 0x177ec,
	0x18ec4, 0x191e1, 0x1afab, 0x1b08e, 0x1cc1a, 0x1d33f,
	0x1ed75, 0x1f250, 0x209d5, 0x216f0, 0x228ba, 0x2379f,
	0x24b0b, 0x2542e, 0x26a64, 0x27541, 0x28c69,
}

func (v qrCodeVersion) formatInfo(maskPattern int) (*Bitset, error) {
	if v.level != Medium {
		return nil, fmt.Errorf("unsupported level %d", v.level)
	}
	if maskPattern < 0 || maskPattern > 7 {
		return nil, fmt.Errorf("invalid maskPattern %d", maskPattern)
	}
	result := NewBitset()
	result.AppendUint32(formatBitSequence[maskPattern&0x7], 15)
	return result, nil
}

func (v qrCodeVersion) versionInfo() *Bitset {
	if v.version < 7 {
		return nil
	}

	result := NewBitset()
	result.AppendUint32(versionBitSequence[v.version], 18)

	return result
}

func (v qrCodeVersion) numDataBits() int {
	numDataBits := 0
	for _, b := range v.block {
		numDataBits += 8 * b.numBlocks * b.numDataCodewords // 8 bits in a byte
	}

	return numDataBits
}

func (v qrCodeVersion) numTerminatorBitsRequired(numDataBits int) int {
	numFreeBits := v.numDataBits() - numDataBits

	var numTerminatorBits int

	switch {
	case numFreeBits >= 4:
		numTerminatorBits = 4
	default:
		numTerminatorBits = numFreeBits
	}

	return numTerminatorBits
}

func (v qrCodeVersion) numBlocks() int {
	numBlocks := 0

	for _, b := range v.block {
		numBlocks += b.numBlocks
	}

	return numBlocks
}

func (v qrCodeVersion) numBitsToPadToCodeword(numDataBits int) int {
	if numDataBits == v.numDataBits() {
		return 0
	}

	return (8 - numDataBits%8) % 8
}

func (v qrCodeVersion) symbolSize() int {
	return 21 + (v.version-1)*4
}
