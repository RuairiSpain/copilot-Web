package view

// NodeClasses returns the style classes of a node: its origin first, then
// collapsed, unhealthy, baselined and diff status classes. Renderers map each
// class to a style and the legend explains each one.
func NodeClasses(n Node) []string {
	var classes []string
	switch n.Origin {
	case OriginAzure:
		classes = append(classes, "azure")
	case OriginBoth:
		classes = append(classes, "both")
	case OriginExternal:
		classes = append(classes, "external")
	case OriginMissing:
		classes = append(classes, "missing")
	case OriginUnobserved:
		classes = append(classes, "unobserved")
	case OriginCollapsed:
		classes = append(classes, "collapsed")
	default:
		classes = append(classes, "local")
	}
	if n.Collapsed && n.Origin != OriginCollapsed {
		classes = append(classes, "collapsed")
	}
	if n.Unhealthy || n.Severity == "error" {
		classes = append(classes, "unhealthy")
	}
	if n.Baselined > 0 {
		classes = append(classes, "baselined")
	}
	switch n.Diff {
	case DiffAdded, DiffRemoved, DiffChanged:
		classes = append(classes, n.Diff)
	}
	return classes
}
