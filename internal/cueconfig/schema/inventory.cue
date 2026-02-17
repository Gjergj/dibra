package schema

#Inventory: {
	// Optional explicit all group. If omitted, top-level groups are treated as children of all.
	all?: #InventoryGroup

	// Top-level group names without an all wrapper.
	[!"all"]?: #InventoryGroup
}

#InventoryGroup: {
	vars?: {[string]: _}
	hosts?: {[string]: {[string]: _}}
	children?: {[string]: #InventoryGroup}
}
