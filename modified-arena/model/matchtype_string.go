// Code generated by "stringer -type=MatchType"; DO NOT EDIT.

package model

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Test-0]
	_ = x[Practice-1]
	_ = x[Qualification-2]
	_ = x[Playoff-3]
}

const _MatchType_name = "TestPracticeQualificationPlayoff"

var _MatchType_index = [...]uint8{0, 4, 12, 25, 32}

func (i MatchType) String() string {
	if i < 0 || i >= MatchType(len(_MatchType_index)-1) {
		return "MatchType(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _MatchType_name[_MatchType_index[i]:_MatchType_index[i+1]]
}
