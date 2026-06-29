package utils

import "math/bits"

// ==========================================
//        以下为极致性能的 256 位位运算辅助函数
// ==========================================

type Word256 [4]uint64

// GetWordMaskLE 产生一个掩码：从 0 到 bitIndex 全为 1，之上全为 0
func GetWordMaskLE(bitIndex uint8) (mask Word256) {
	idx := bitIndex / 64
	shift := bitIndex % 64
	for i := uint8(0); i < idx; i++ {
		mask[i] = ^uint64(0)
	}
	if shift == 63 {
		mask[idx] = ^uint64(0)
	} else {
		mask[idx] = (uint64(1) << (shift + 1)) - 1
	}
	return
}

// GetWordMaskGE 产生一个掩码：从 bitIndex 到 255 全为 1，之下全为 0
func GetWordMaskGE(bitIndex uint8) (mask Word256) {
	idx := bitIndex / 64
	shift := bitIndex % 64
	if shift == 0 {
		mask[idx] = ^uint64(0)
	} else {
		mask[idx] = ^((uint64(1) << shift) - 1)
	}
	for i := idx + 1; i < 4; i++ {
		mask[i] = ^uint64(0)
	}
	return
}

func AndWord(a, b Word256) Word256 {
	return Word256{a[0] & b[0], a[1] & b[1], a[2] & b[2], a[3] & b[3]}
}

func IsWordZero(w Word256) bool {
	return w[0] == 0 && w[1] == 0 && w[2] == 0 && w[3] == 0
}

// MostSignificantBit 寻找 256 位中最高的 1 所在的索引 (0-255)
func MostSignificantBit(w Word256) uint8 {
	if w[3] != 0 {
		return 192 + uint8(63-bits.LeadingZeros64(w[3]))
	}
	if w[2] != 0 {
		return 128 + uint8(63-bits.LeadingZeros64(w[2]))
	}
	if w[1] != 0 {
		return 64 + uint8(63-bits.LeadingZeros64(w[1]))
	}
	return uint8(63 - bits.LeadingZeros64(w[0]))
}

// LeastSignificantBit 寻找 256 位中最低的 1 所在的索引 (0-255)
func LeastSignificantBit(w Word256) uint8 {
	if w[0] != 0 {
		return uint8(bits.TrailingZeros64(w[0]))
	}
	if w[1] != 0 {
		return 64 + uint8(bits.TrailingZeros64(w[1]))
	}
	if w[2] != 0 {
		return 128 + uint8(bits.TrailingZeros64(w[2]))
	}
	return 192 + uint8(bits.TrailingZeros64(w[3]))
}

// SetBitmapBit 根据 Tick 物理位置直接设置或清除位图中的 Bit
// tick: 物理 tick（未压缩）
// initialized: true 代表该 Tick 有流动性（置 1），false 代表无流动性（清 0）
func SetBitmapBit(tick, tickSpacing int32, initialized bool, bitmap map[uint16]Word256) {
	// 1. 根据 tickSpacing 计算压缩坐标
	compressed := tick / tickSpacing
	if tick < 0 && tick%tickSpacing != 0 {
		compressed--
	}

	// 2. 计算在二级位图中的 Word 索引和 Bit 索引
	wordIndex := uint16(compressed >> 8)
	bitIndex := uint8(compressed & 0xFF)

	// 3. 定位到 [4]uint64 数组中的具体哪个 uint64，以及内部的偏移量
	word64Idx := bitIndex / 64
	bit64Shift := bitIndex % 64

	// 4. 获取当前 Word 状态（若不存在则自动初始化为全 0）
	word := bitmap[wordIndex]

	// 5. 根据初始化状态，精确定位到该位并进行强制赋值
	if initialized {
		// === 置 1 (按位或运算 OR) ===
		word[word64Idx] |= uint64(1) << bit64Shift
	} else {
		// === 清 0 (按位与非运算 AND NOT) ===
		word[word64Idx] &= ^(uint64(1) << bit64Shift)
	}

	// 6. 写回内存
	bitmap[wordIndex] = word
}
