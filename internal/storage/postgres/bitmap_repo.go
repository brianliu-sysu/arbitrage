package postgres

import (
	"context"
	"math/big"
)

// BitmapRepo tick bitmap 持久化（预留，当前由内存 State.Bitmap 维护）。
type BitmapRepo struct{}

// NewBitmapRepo 创建 BitmapRepo。
func NewBitmapRepo() *BitmapRepo { return &BitmapRepo{} }

func (BitmapRepo) SaveWord(context.Context, string, string, int16, *big.Int) error {
	return nil
}
