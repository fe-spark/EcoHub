package model

// Business logic symbolic constants (to avoid hardcoding Chinese in logic checks)
const (
	TagOthersValue  = "__others__"
	TagOthersName   = "其他"
	TagUnknownValue = "__unknown__"
	TagUnknownName  = "未知"
)

const (
	TagUncategorizedValue int64 = -1
	TagUncategorizedName        = "未细分"
)

// Standard Big Categories (顶级大类)
const (
	BigCategoryMovie       = "电影"
	BigCategoryTV          = "电视剧"
	BigCategoryVariety     = "综艺"
	BigCategoryAnimation   = "动漫"
	BigCategoryDocumentary = "纪录片"
	BigCategoryShortFilm   = "短剧"
	BigCategoryOther       = "其他"
)
