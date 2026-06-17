package stack

// WithLock runs fn against a freshly-loaded copy of the named stack while
// holding the stack's advisory lock, then saves the stack. The lock is held
// across load+mutate+save so concurrent sdf processes cannot lose updates.
// The stack is saved only when fn returns nil.
func WithLock(root, stackID string, fn func(*Stack) error) error {
	lock, err := AcquireLock(root, stackID, LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	s, err := LoadStack(root, stackID)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return Save(root, s)
}

// localLockID is the stack-lock "name" used for the repo-wide local.json lock.
// AcquireLock turns it into the lock file .sdf/__local__.lock, distinct from any
// real stack lock file (stack IDs never start with "__").
const localLockID = "__local__"

// WithLocalLock runs fn against a freshly-loaded LocalState while holding the
// repo-wide local.json advisory lock, then saves it. The lock is held across
// load+mutate+save so concurrent sdf processes (even on DIFFERENT stacks, or
// prune vs. a worktree sync) cannot lose updates to .sdf/local.json. LocalState
// is saved only when fn returns nil.
//
// DEADLOCK RULE: a stack lock (WithLock/AcquireLock) MAY be held while calling
// WithLocalLock (stack-OUTER, local-INNER). The reverse is FORBIDDEN: fn must
// NEVER acquire a stack lock, load+mutate a stack, or run git/network/gh work.
// Keep fn tiny — mutate only LocalState fields.
func WithLocalLock(root string, fn func(*LocalState) error) error {
	lock, err := AcquireLock(root, localLockID, LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	ls, err := LoadLocal(root)
	if err != nil {
		return err
	}
	if err := fn(ls); err != nil {
		return err
	}
	return SaveLocal(root, ls)
}
