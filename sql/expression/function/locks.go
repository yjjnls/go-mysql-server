package function

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/src-d/go-errors.v1"

	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/expression"
)

// ErrIllegalLockNameArgType is a kind of error that is thrown when the parameter passed as a lock name is not a string.
var ErrIllegalLockNameArgType = errors.NewKind("Illegal parameter data type %s for operation '%s'")

// ReleaseAllLocksForLS returns the logic to execute when the sql function release_all_locks is executed
func ReleaseAllLocksForLS(ls *sql.LockSubsystem) sql.EvalLogic {
	return func(ctx *sql.Context, _ sql.Row) (interface{}, error) {
		return ls.ReleaseAll(ctx)
	}
}

// NamedLockFuncLogic is the logic executed when one of the single argument named lock functions is executeed
type NamedLockFuncLogic func(ctx *sql.Context, ls *sql.LockSubsystem, lockName string) (interface{}, error)

// NamedLockFunction is a sql function that takes just the name of a lock as an argument
type NamedLockFunction struct {
	expression.UnaryExpression
	ls       *sql.LockSubsystem
	funcName string
	retType  sql.Type
	logic    NamedLockFuncLogic
}

var _ sql.FunctionExpression = (*NamedLockFunction)(nil)

// NewNamedLockFunc creates a NamedLockFunction
func NewNamedLockFunc(ls *sql.LockSubsystem, funcName string, retType sql.Type, logic NamedLockFuncLogic) sql.Function1 {
	fn := func(e sql.Expression) sql.Expression {
		return &NamedLockFunction{expression.UnaryExpression{Child: e}, ls, funcName, retType, logic}
	}

	return sql.Function1{Name: funcName, Fn: fn}
}

// FunctionName implements sql.FunctionExpression
func (nl *NamedLockFunction) FunctionName() string {
	return nl.funcName
}

// Eval implements the Expression interface.
func (nl *NamedLockFunction) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	if nl.Child == nil {
		return nil, nil
	}

	val, err := nl.Child.Eval(ctx, row)

	if err != nil {
		return nil, err
	}

	if val == nil {
		return nil, nil
	}

	lockName, ok := val.(string)

	if !ok {
		return nil, ErrIllegalLockNameArgType.New(nl.Child.Type().String(), nl.funcName)
	}

	return nl.logic(ctx, nl.ls, lockName)
}

// String implements the fmt.Stringer interface.
func (nl *NamedLockFunction) String() string {
	return fmt.Sprintf("%s(%s)", strings.ToUpper(nl.funcName), nl.Child.String())
}

// IsNullable implements the Expression interface.
func (nl *NamedLockFunction) IsNullable() bool {
	return nl.Child.IsNullable()
}

// WithChildren implements the Expression interface.
func (nl *NamedLockFunction) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(nl, len(children), 1)
	}

	return &NamedLockFunction{expression.UnaryExpression{Child: children[0]}, nl.ls, nl.funcName, nl.retType, nl.logic}, nil
}

// Type implements the Expression interface.
func (nl *NamedLockFunction) Type() sql.Type {
	return nl.retType
}

// ReleaseLockFunc is the function logic that is executed when the release_lock function is called.
func ReleaseLockFunc(ctx *sql.Context, ls *sql.LockSubsystem, lockName string) (interface{}, error) {
	err := ls.Unlock(ctx, lockName)

	if err != nil {
		if sql.ErrLockDoesNotExist.Is(err) {
			return nil, nil
		} else if sql.ErrLockNotOwned.Is(err) {
			return int8(0), nil
		}

		return nil, err
	}

	return int8(1), nil
}

// IsFreeLockFunc is the function logic that is executed when the is_free_lock function is called.
func IsFreeLockFunc(_ *sql.Context, ls *sql.LockSubsystem, lockName string) (interface{}, error) {
	state, _ := ls.GetLockState(lockName)

	switch state {
	case sql.LockInUse:
		return int8(0), nil
	default: // return 1 if the lock is free.  If the lock doesn't exist yet it is free
		return int8(1), nil
	}
}

// IsUsedLockFunc is the function logic that is executed when the is_used_lock function is called.
func IsUsedLockFunc(ctx *sql.Context, ls *sql.LockSubsystem, lockName string) (interface{}, error) {
	state, owner := ls.GetLockState(lockName)

	switch state {
	case sql.LockInUse:
		return owner, nil
	default:
		return nil, nil
	}
}

// GetLock is a SQL function implementing get_lock
type GetLock struct {
	expression.BinaryExpression
	ls *sql.LockSubsystem
}

var _ sql.FunctionExpression = (*GetLock)(nil)

// CreateNewGetLock returns a new GetLock object
func CreateNewGetLock(ls *sql.LockSubsystem) func(e1, e2 sql.Expression) sql.Expression {
	return func(e1, e2 sql.Expression) sql.Expression {
		return &GetLock{expression.BinaryExpression{e1, e2}, ls}
	}
}

// FunctionName implements sql.FunctionExpression
func (gl *GetLock) FunctionName() string {
	return "get_lock"
}

// Eval implements the Expression interface.
func (gl *GetLock) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	if gl.Left == nil {
		return nil, nil
	}

	leftVal, err := gl.Left.Eval(ctx, row)

	if err != nil {
		return nil, err
	}

	if leftVal == nil {
		return nil, nil
	}

	if gl.Right == nil {
		return nil, nil
	}

	rightVal, err := gl.Right.Eval(ctx, row)

	if err != nil {
		return nil, err
	}

	if rightVal == nil {
		return nil, nil
	}

	lockName, ok := leftVal.(string)

	if !ok {
		return nil, ErrIllegalLockNameArgType.New(gl.Left.Type().String(), "get_lock")
	}

	timeout, err := sql.Int64.Convert(rightVal)

	if err != nil {
		return nil, fmt.Errorf("illegal value for timeout %v", timeout)
	}

	err = gl.ls.Lock(ctx, lockName, time.Second*time.Duration(timeout.(int64)))

	if err != nil {
		if sql.ErrLockTimeout.Is(err) {
			return int8(0), nil
		}

		return nil, err
	}

	return int8(1), nil
}

// String implements the fmt.Stringer interface.
func (gl *GetLock) String() string {
	return fmt.Sprintf("get_lock(%s, %s)", gl.Left.String(), gl.Right.String())
}

// IsNullable implements the Expression interface.
func (gl *GetLock) IsNullable() bool {
	return false
}

// WithChildren implements the Expression interface.
func (gl *GetLock) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 2 {
		return nil, sql.ErrInvalidChildrenNumber.New(gl, len(children), 1)
	}

	return &GetLock{expression.BinaryExpression{Left: children[0], Right: children[1]}, gl.ls}, nil
}

// Type implements the Expression interface.
func (gl *GetLock) Type() sql.Type {
	return sql.Int8
}

type ReleaseAllLocks struct {
	NoArgFunc
	ls *sql.LockSubsystem
}

var _ sql.FunctionExpression = ReleaseAllLocks{}

func NewReleaseAllLocks(ls *sql.LockSubsystem) func() sql.Expression {
	return func() sql.Expression {
		return ReleaseAllLocks{
			NoArgFunc: NoArgFunc{"release_all_locks", sql.Int32},
			ls: ls,
		}
	}
}

func (r ReleaseAllLocks) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	return r.ls.ReleaseAll(ctx)
}

func (r ReleaseAllLocks) WithChildren(expressions ...sql.Expression) (sql.Expression, error) {
	return NoArgFuncWithChildren(r, expressions)
}