package hda

// ErrBusy 已有查询在运行。
type ErrBusy struct{}

func (ErrBusy) Error() string { return "已有查询在运行" }
