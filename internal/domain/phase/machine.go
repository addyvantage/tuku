package phase

// Machine enforces legal phase transitions.
type Machine interface {
	CanTransition(from Phase, to Phase, trigger TransitionTrigger) error
}
