package psrule

// RunScriptForTest exposes the fixed run command so tests can assert nothing is interpolated into it.
func RunScriptForTest() string { return runScript }

// DetectScriptForTest exposes the fixed detection command.
func DetectScriptForTest() string { return detectScript }
