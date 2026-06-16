package project

// AllowedIcons is the canonical set of icon identifiers accepted for both
// areas and projects. It is a superset of the frontend's curated picker
// (src/lib/types/icons.ts) plus generic and forward-looking options, so the
// backend never bottlenecks the UI. The area domain references this same set
// to keep the two from drifting.
var AllowedIcons = map[string]bool{
	// Frontend curated picker (must all stay valid).
	"folder": true, "briefcase": true, "user": true, "heart": true,
	"dollar-sign": true, "book": true, "lightbulb": true, "rocket": true,
	"palette": true, "music": true, "camera": true, "film": true,
	"gift": true, "calendar": true, "shopping-cart": true, "map": true,
	"plane": true, "tools": true, "code": true, "cpu": true,
	"shield": true, "trophy": true, "chat": true, "clipboard": true,
	"flag": true, "star": true, "leaf": true, "paw": true,
	"heart-pulse": true, "globe": true,

	// Generic options retained from the original backend allowlist.
	"inbox": true, "alarm": true, "search": true, "settings": true,
	"menu": true, "layer": true, "zap": true, "computer": true,
	"sprout": true, "bone": true,

	// Extra headroom for future themed areas/projects.
	"home": true, "building": true, "wrench": true, "target": true,
	"bookmark": true, "bell": true, "mail": true, "phone": true,
}
