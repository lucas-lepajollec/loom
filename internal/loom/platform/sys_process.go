package platform

const (
	CreateNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	DetachedProcess       = 0x00000008 // DETACHED_PROCESS
	CreateNoWindow        = 0x08000000 // CREATE_NO_WINDOW (aucune console pour l'enfant)
)
