package alias

// Rename carries the alias of an old session name over to its new name, for
// a worktree whose branch and folder were renamed. Without an alias for the
// old name it changes nothing, so an unrelated label already stored under the
// new name is not touched. It returns whether an alias moved.
func Rename(path, from, to string) (bool, error) {
	aliases := Load(path)
	name, ok := aliases[from]
	if !ok || from == to {
		return false, nil
	}
	delete(aliases, from)
	aliases[to] = name
	return true, save(path, aliases)
}
