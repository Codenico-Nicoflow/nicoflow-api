package habit

// The subject catalog.
//
// A subject is cosmetic: it drives the card's icon and nothing else. It must
// never influence scheduling, targets or units — a user who picks "reading" can
// still set any schedule they like. Subject-driven *prefills* are a reasonable
// later touch; subject-driven constraints are not.
//
// The list is served rather than pinned in a Postgres enum for two reasons.
// Adding a subject stays a data change instead of a migration, and — more
// importantly — an older mobile build (E-033 ships on a slower cadence) that
// meets an unknown slug renders a fallback icon instead of a blank card. Same
// rule the notification types already follow.
//
// Slugs are stable forever. v1 renders them as Lucide icons; v2 swaps in
// commissioned artwork, and because the slug is the contract that swap is pure
// asset work — no migration, no contract change.

// SubjectView is one catalog entry. LabelKey is an i18n key, not a display
// string: the server does not know the caller's language, and shipping English
// here would force every client to re-translate it.
type SubjectView struct {
	Slug     string `json:"slug" validate:"required"`
	LabelKey string `json:"labelKey" validate:"required"`
}

// SubjectCatalog is the canonical set. Ordered deliberately — build habits
// first, quit habits after — so a client that renders it verbatim gets a sane
// grouping without knowing the semantics.
var SubjectCatalog = []SubjectView{
	{Slug: "reading", LabelKey: "habits.subject.reading"},
	{Slug: "exercise", LabelKey: "habits.subject.exercise"},
	{Slug: "running", LabelKey: "habits.subject.running"},
	{Slug: "walking", LabelKey: "habits.subject.walking"},
	{Slug: "stretching", LabelKey: "habits.subject.stretching"},
	{Slug: "meditation", LabelKey: "habits.subject.meditation"},
	{Slug: "hydration", LabelKey: "habits.subject.hydration"},
	{Slug: "nutrition", LabelKey: "habits.subject.nutrition"},
	{Slug: "sleep", LabelKey: "habits.subject.sleep"},
	{Slug: "journaling", LabelKey: "habits.subject.journaling"},
	{Slug: "gratitude", LabelKey: "habits.subject.gratitude"},
	{Slug: "study", LabelKey: "habits.subject.study"},
	{Slug: "language", LabelKey: "habits.subject.language"},
	{Slug: "coding", LabelKey: "habits.subject.coding"},
	{Slug: "music", LabelKey: "habits.subject.music"},
	{Slug: "savings", LabelKey: "habits.subject.savings"},
	{Slug: "tidying", LabelKey: "habits.subject.tidying"},
	{Slug: "screen_time", LabelKey: "habits.subject.screenTime"},
	{Slug: "quit_smoking", LabelKey: "habits.subject.quitSmoking"},
	{Slug: "quit_drinking", LabelKey: "habits.subject.quitDrinking"},
	{Slug: "quit_sugar", LabelKey: "habits.subject.quitSugar"},
	{Slug: DefaultSubject, LabelKey: "habits.subject.custom"},
}
