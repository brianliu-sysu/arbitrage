package protocol

import "context"

type preparedBlockFuncs struct {
	apply    func(context.Context) error
	rollback func(context.Context) error
}

func (f *preparedBlockFuncs) Apply(ctx context.Context) error {
	return f.apply(ctx)
}

func (f *preparedBlockFuncs) Rollback(ctx context.Context) error {
	if f.rollback == nil {
		return nil
	}
	return f.rollback(ctx)
}

func newPreparedBlock(apply, rollback func(context.Context) error) PreparedBlock {
	return &preparedBlockFuncs{apply: apply, rollback: rollback}
}
