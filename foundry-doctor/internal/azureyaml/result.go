package azureyaml

import "github.com/ruairispain/copilot-web/foundry-doctor/internal/model"

// The result types live in internal/model so model.Input can carry them without an import cycle.
// These aliases keep the package API the same.

type Result = model.YAMLAnalysis
type Duplicate = model.Duplicate
type Form = model.Form
type Interpolation = model.Interpolation
type ExtensionBlock = model.ExtensionBlock
type Level = model.Level
type IssueCode = model.IssueCode
type Issue = model.Issue
type UndefinedUse = model.UndefinedUse

const (
	FormEnv                = model.FormEnv
	FormEnvDefault         = model.FormEnvDefault
	FormEnvOperator        = model.FormEnvOperator
	FormFoundry            = model.FormFoundry
	FormEscapedEnv         = model.FormEscapedEnv
	FormEscapedFoundry     = model.FormEscapedFoundry
	FormMalformed          = model.FormMalformed
	LevelError             = model.LevelError
	LevelInfo              = model.LevelInfo
	CodeMissingRequired    = model.CodeMissingRequired
	CodeInvalidType        = model.CodeInvalidType
	CodeInvalidEnum        = model.CodeInvalidEnum
	CodePatternMismatch    = model.CodePatternMismatch
	CodeLength             = model.CodeLength
	CodeMinProperties      = model.CodeMinProperties
	CodeUnknownProperty    = model.CodeUnknownProperty
	CodeForbiddenProperty  = model.CodeForbiddenProperty
	CodeUnsupportedShape   = model.CodeUnsupportedShape
	CodeUnknownTopLevelKey = model.CodeUnknownTopLevelKey
	CodeUnknownHost        = model.CodeUnknownHost
	CodeAnchor             = model.CodeAnchor
	CodeAlias              = model.CodeAlias
	CodeMergeKey           = model.CodeMergeKey
	CodeNonScalarKey       = model.CodeNonScalarKey
)
