package dataitem

import commondataitem "github.com/addp/common/dataitem"

type Organization = commondataitem.Organization

const (
	OrganizationSingle = commondataitem.OrganizationSingle
	OrganizationMulti  = commondataitem.OrganizationMulti
	OrganizationWhole  = commondataitem.OrganizationWhole
)

type DataType = commondataitem.DataType

const (
	DataTypeTable     = commondataitem.DataTypeTable
	DataTypeDocument  = commondataitem.DataTypeDocument
	DataTypeMedia     = commondataitem.DataTypeMedia
	DataTypeContainer = commondataitem.DataTypeContainer
	DataTypeGraph     = commondataitem.DataTypeGraph
	DataTypeUnknown   = commondataitem.DataTypeUnknown
)

type ComponentMatchScope = commondataitem.ComponentMatchScope

const (
	ComponentMatchScopeSameDirectory = commondataitem.ComponentMatchScopeSameDirectory
	ComponentMatchScopeSamePrefix    = commondataitem.ComponentMatchScopeSamePrefix
	ComponentMatchScopeRecursive     = commondataitem.ComponentMatchScopeRecursive
)

type ComponentMatchKey = commondataitem.ComponentMatchKey

const (
	ComponentMatchKeyBaseName = commondataitem.ComponentMatchKeyBaseName
	ComponentMatchKeyManifest = commondataitem.ComponentMatchKeyManifest
)

type EntryRule = commondataitem.EntryRule
type ComponentRule = commondataitem.ComponentRule
type ContainerRule = commondataitem.ContainerRule
type WholeScopeRule = commondataitem.WholeScopeRule
type FormatRule = commondataitem.FormatRule
