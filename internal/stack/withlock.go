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
