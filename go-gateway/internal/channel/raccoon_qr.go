package channel

import (
	"fmt"
	"strconv"
	"strings"
)

// Raccoon 登录页的二维码是**本地生成**的纯 SVG（官方客户端 raccoon-qr.ts 也是自己
// 渲染），服务端不提供二维码图片：二维码内容只是 `buildQrImageUrl` 拼出来的 URL，
// 里面没有任何签名/加密。因此这里把官方客户端的渲染器按字节等价地移植成 Go：
// 纠错等级固定 M、掩码按标准惩罚函数自选，输出与 JS 版逐一比特一致
// （由 TestBuildRaccoonQrMatrixMatchesReference 用参考实现算出的 SHA-256 钉住）。
//
// 之所以自己移植而不是引第三方库：这段渲染逻辑只服务于「把登录 URL 画成二维码」，
// 引库会把通用编解码器的行为与官方客户端解耦，一旦官方调整掩码/版本选择就再也对不上；
// 移植版则能直接对着参考实现回归。

// raccoonQrDataCodewords 是版本 1..10 在纠错等级 M 下的数据码字数（含 EC 码字前的
// 全部数据码字）。索引 0 未使用。
var raccoonQrDataCodewords = [11]int{0, 16, 28, 44, 64, 86, 108, 124, 154, 182, 216}

// raccoonQrECBlocks 是版本 1..10 的 EC 分块表：每块 EC 码字数 + 若干 [块数, 每块数据码字数]。
type raccoonQrECBlock struct {
	ecPerBlock int
	groups     [][2]int
}

var raccoonQrECBlocks = [11]*raccoonQrECBlock{
	nil,
	{10, [][2]int{{1, 16}}},
	{16, [][2]int{{1, 28}}},
	{26, [][2]int{{1, 44}}},
	{18, [][2]int{{2, 32}}},
	{24, [][2]int{{2, 43}}},
	{16, [][2]int{{4, 27}}},
	{18, [][2]int{{4, 31}}},
	{22, [][2]int{{2, 38}, {2, 39}}},
	{22, [][2]int{{3, 36}, {2, 37}}},
	{26, [][2]int{{4, 43}, {1, 44}}},
}

// GF(2^8) 的指数/对数表，本原多项式 0x11D（x^8+x^4+x^3+x^2+1），与 JS 版的 285 一致。
var (
	raccoonGFExp [512]byte
	raccoonGFLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		raccoonGFExp[i] = byte(x)
		raccoonGFLog[x] = byte(i)
		x <<= 1
		if x&256 != 0 {
			x ^= 285
		}
	}
	for i := 255; i < 512; i++ {
		raccoonGFExp[i] = raccoonGFExp[i-255]
	}
}

func raccoonGFMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return raccoonGFExp[int(raccoonGFLog[a])+int(raccoonGFLog[b])]
}

func raccoonPolyMul(a, b []byte) []byte {
	result := make([]byte, len(a)+len(b)-1)
	for i := range a {
		for j := range b {
			result[i+j] ^= raccoonGFMul(a[i], b[j])
		}
	}
	return result
}

func raccoonRSGeneratorPoly(degree int) []byte {
	poly := []byte{1}
	for i := 0; i < degree; i++ {
		poly = raccoonPolyMul(poly, []byte{1, raccoonGFExp[i]})
	}
	return poly
}

// raccoonRSEncode 对一块数据码字做 Reed-Solomon 纠错编码，返回 ecCount 个 EC 码字。
func raccoonRSEncode(data []byte, ecCount int) []byte {
	gen := raccoonRSGeneratorPoly(ecCount)
	buf := make([]byte, len(data)+ecCount)
	copy(buf, data)
	for i := 0; i < len(data); i++ {
		coef := buf[i]
		if coef == 0 {
			continue
		}
		for j := range gen {
			buf[i+j] ^= raccoonGFMul(gen[j], coef)
		}
	}
	return buf[len(data):]
}

// raccoonPickVersion 选最小可容纳 byteLength 字节的版本；超出上限返回 0。
func raccoonPickVersion(byteLength int) int {
	for version := 1; version <= 10; version++ {
		capacityBits := raccoonQrDataCodewords[version] * 8
		overheadBits := 4
		if version > 9 {
			overheadBits = 4 + 16
		} else {
			overheadBits = 4 + 8
		}
		if overheadBits+byteLength*8 <= capacityBits {
			return version
		}
	}
	return 0
}

// raccoonQrMatrix 是渲染结果：尺寸（每边模块数）与模块矩阵（true = 深色）。
type raccoonQrMatrix struct {
	Size    int
	Modules [][]bool
}

// buildRaccoonQrMatrix 生成二维码矩阵（纯本地、确定性）。
func buildRaccoonQrMatrix(text string) (*raccoonQrMatrix, error) {
	bytes := []byte(text)
	version := raccoonPickVersion(len(bytes))
	if version == 0 {
		return nil, fmt.Errorf("raccoon: 二维码内容过长（%d 字节，上限 213 字节），请缩短内容", len(bytes))
	}
	size := version*4 + 17
	modules := make([][]bool, size)
	isFunction := make([][]bool, size)
	for i := range modules {
		modules[i] = make([]bool, size)
		isFunction[i] = make([]bool, size)
	}
	raccoonDrawFunctionPatterns(modules, isFunction, size, version)
	raccoonDrawCodewords(modules, isFunction, size, raccoonBuildCodewords(bytes, version))

	bestMask := 0
	bestPenalty := int(^uint(0) >> 1)
	for mask := 0; mask < 8; mask++ {
		raccoonApplyMask(modules, isFunction, size, mask)
		raccoonDrawFormatBits(modules, isFunction, size, mask)
		penalty := raccoonComputePenalty(modules, size)
		if penalty < bestPenalty {
			bestPenalty = penalty
			bestMask = mask
		}
		raccoonApplyMask(modules, isFunction, size, mask)
	}
	raccoonApplyMask(modules, isFunction, size, bestMask)
	raccoonDrawFormatBits(modules, isFunction, size, bestMask)
	return &raccoonQrMatrix{Size: size, Modules: modules}, nil
}

func raccoonBuildCodewords(bytes []byte, version int) []byte {
	blocks := raccoonQrECBlocks[version]
	if blocks == nil {
		panic(fmt.Sprintf("raccoon: 不支持的二维码版本 %d", version))
	}
	totalDataCodewords := raccoonQrDataCodewords[version]
	capacityBits := totalDataCodewords * 8
	bits := make([]int, 0, capacityBits)
	pushBits := func(value, length int) {
		for i := length - 1; i >= 0; i-- {
			bits = append(bits, (value>>i)&1)
		}
	}
	pushBits(4, 4) // 字节模式
	lenBits := 8
	if version > 9 {
		lenBits = 16
	}
	pushBits(len(bytes), lenBits)
	for _, b := range bytes {
		pushBits(int(b), 8)
	}
	terminator := 4
	if capacityBits-len(bits) < terminator {
		terminator = capacityBits - len(bits)
	}
	pushBits(0, terminator)
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	padBytes := [2]int{236, 17}
	for i := 0; len(bits) < capacityBits; i++ {
		pushBits(padBytes[i%2], 8)
	}
	dataCodewords := make([]byte, 0, len(bits)/8)
	for i := 0; i < len(bits); i += 8 {
		b := 0
		for j := 0; j < 8; j++ {
			b = b<<1 | bits[i+j]
		}
		dataCodewords = append(dataCodewords, byte(b))
	}

	dataBlocks := make([][]byte, 0)
	ecBlocks := make([][]byte, 0)
	offset := 0
	for _, group := range blocks.groups {
		count, dataPerBlock := group[0], group[1]
		for b := 0; b < count; b++ {
			block := dataCodewords[offset : offset+dataPerBlock]
			offset += dataPerBlock
			dataBlocks = append(dataBlocks, block)
			ecBlocks = append(ecBlocks, raccoonRSEncode(block, blocks.ecPerBlock))
		}
	}
	result := make([]byte, 0)
	maxDataLen := 0
	for _, b := range dataBlocks {
		if len(b) > maxDataLen {
			maxDataLen = len(b)
		}
	}
	for i := 0; i < maxDataLen; i++ {
		for _, block := range dataBlocks {
			if i < len(block) {
				result = append(result, block[i])
			}
		}
	}
	for i := 0; i < blocks.ecPerBlock; i++ {
		for _, block := range ecBlocks {
			result = append(result, block[i])
		}
	}
	return result
}

func raccoonAlignmentPositions(version, size int) []int {
	if version == 1 {
		return nil
	}
	numAlign := version/7 + 2
	step := ((version*4 + 4 + (numAlign*2 - 3)) / (numAlign*2 - 2)) * 2
	result := []int{6}
	for pos := size - 7; len(result) < numAlign; pos -= step {
		result = append(result[:1], append([]int{pos}, result[1:]...)...)
	}
	return result
}

func raccoonDrawFinderPattern(modules, isFunction [][]bool, size, x, y int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			xx, yy := x+dx, y+dy
			if xx < 0 || xx >= size || yy < 0 || yy >= size {
				continue
			}
			dist := raccoonAbs(dx)
			if raccoonAbs(dy) > dist {
				dist = raccoonAbs(dy)
			}
			modules[yy][xx] = dist != 2 && dist != 4
			isFunction[yy][xx] = true
		}
	}
}

func raccoonDrawAlignmentPattern(modules, isFunction [][]bool, x, y int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			dist := raccoonAbs(dx)
			if raccoonAbs(dy) > dist {
				dist = raccoonAbs(dy)
			}
			modules[y+dy][x+dx] = dist != 1
			isFunction[y+dy][x+dx] = true
		}
	}
}

func raccoonDrawFunctionPatterns(modules, isFunction [][]bool, size, version int) {
	for i := 0; i < size; i++ {
		dark := i%2 == 0
		modules[6][i] = dark
		isFunction[6][i] = true
		modules[i][6] = dark
		isFunction[i][6] = true
	}
	raccoonDrawFinderPattern(modules, isFunction, size, 3, 3)
	raccoonDrawFinderPattern(modules, isFunction, size, size-4, 3)
	raccoonDrawFinderPattern(modules, isFunction, size, 3, size-4)
	positions := raccoonAlignmentPositions(version, size)
	n := len(positions)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			isCorner := (i == 0 && j == 0) || (i == 0 && j == n-1) || (i == n-1 && j == 0)
			if isCorner {
				continue
			}
			raccoonDrawAlignmentPattern(modules, isFunction, positions[i], positions[j])
		}
	}
	raccoonDrawFormatBits(modules, isFunction, size, 0)
	if version >= 7 {
		rem := version
		for i := 0; i < 12; i++ {
			rem = rem<<1 ^ (rem>>11)*7973
		}
		bits := version<<12 | rem
		for i := 0; i < 18; i++ {
			dark := (bits>>i)&1 == 1
			a := size - 11 + i%3
			b := i / 3
			modules[b][a] = dark
			isFunction[b][a] = true
			modules[a][b] = dark
			isFunction[a][b] = true
		}
	}
}

func raccoonDrawFormatBits(modules, isFunction [][]bool, size, mask int) {
	data := 0<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = rem<<1 ^ (rem>>9)*1335
	}
	bits := (data<<10 | rem) ^ 21522
	set := func(x, y int, dark bool) {
		modules[y][x] = dark
		isFunction[y][x] = true
	}
	bit := func(i int) bool { return (bits>>i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		set(8, i, bit(i))
	}
	set(8, 7, bit(6))
	set(8, 8, bit(7))
	set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		set(size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		set(8, size-7+(i-8), bit(i))
	}
	set(8, size-8, true)
}

func raccoonDrawCodewords(modules, isFunction [][]bool, size int, codewords []byte) {
	i := 0
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := (right+1)&2 == 0
				y := vert
				if upward {
					y = size - 1 - vert
				}
				if !isFunction[y][x] && i < len(codewords)*8 {
					b := codewords[i>>3]
					modules[y][x] = (b>>(7-(i&7)))&1 == 1
					i++
				}
			}
		}
	}
}

func raccoonApplyMask(modules, isFunction [][]bool, size, mask int) {
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if isFunction[y][x] {
				continue
			}
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (y/2+x/3)%2 == 0
			case 5:
				invert = x*y%2+x*y%3 == 0
			case 6:
				invert = (x*y%2+x*y%3)%2 == 0
			default:
				invert = ((x+y)%2+x*y%3)%2 == 0
			}
			if invert {
				modules[y][x] = !modules[y][x]
			}
		}
	}
}

func raccoonComputePenalty(modules [][]bool, size int) int {
	const (
		n1 = 3
		n2 = 3
		n3 = 40
		n4 = 10
	)
	result := 0
	for y := 0; y < size; y++ {
		line := make([]bool, size)
		copy(line, modules[y])
		result += raccoonPenaltyForLine(line, size, n1, n3)
	}
	for x := 0; x < size; x++ {
		line := make([]bool, size)
		for y := 0; y < size; y++ {
			line[y] = modules[y][x]
		}
		result += raccoonPenaltyForLine(line, size, n1, n3)
	}
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			c := modules[y][x]
			if c == modules[y][x+1] && c == modules[y+1][x] && c == modules[y+1][x+1] {
				result += n2
			}
		}
	}
	dark := 0
	for _, row := range modules {
		for _, cell := range row {
			if cell {
				dark++
			}
		}
	}
	total := size * size
	k := (raccoonAbs(dark*20-total*10)+total-1)/total - 1
	if k < 0 {
		k = 0
	}
	result += k * n4
	return result
}

func raccoonPenaltyForLine(line []bool, size, n1, n3 int) int {
	result := 0
	runLength := 1
	for i := 1; i < size; i++ {
		if line[i] == line[i-1] {
			runLength++
		} else {
			if runLength >= 5 {
				result += n1 + (runLength - 5)
			}
			runLength = 1
		}
	}
	if runLength >= 5 {
		result += n1 + (runLength - 5)
	}
	patternA := [11]bool{true, false, true, true, true, false, true, false, false, false, false}
	patternB := [11]bool{false, false, false, false, true, false, true, true, true, false, true}
	for i := 0; i+11 <= size; i++ {
		matchA, matchB := true, true
		for j := 0; j < 11; j++ {
			if line[i+j] != patternA[j] {
				matchA = false
			}
			if line[i+j] != patternB[j] {
				matchB = false
			}
			if !matchA && !matchB {
				break
			}
		}
		if matchA {
			result += n3
		}
		if matchB {
			result += n3
		}
	}
	return result
}

// renderRaccoonQrSVG 把文本渲染成二维码 SVG。px 是边长像素，margin 是静区模块数。
func renderRaccoonQrSVG(text string, px, margin int) (string, error) {
	matrix, err := buildRaccoonQrMatrix(text)
	if err != nil {
		return "", err
	}
	dim := matrix.Size + margin*2
	var segments strings.Builder
	for y := 0; y < matrix.Size; y++ {
		for x := 0; x < matrix.Size; x++ {
			if matrix.Modules[y][x] {
				segments.WriteString("M")
				segments.WriteString(strconv.Itoa(x + margin))
				segments.WriteString(",")
				segments.WriteString(strconv.Itoa(y + margin))
				segments.WriteString("h1v1h-1z")
			}
		}
	}
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img"><rect width="%d" height="%d" fill="#ffffff"/><path d="%s" fill="#000000"/></svg>`,
		px, px, dim, dim, dim, dim, segments.String(),
	), nil
}

func raccoonAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
