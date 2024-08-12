package enricher

func StoreToPublic(store FindStore) map[string]map[string][]string {
	public := make(map[string]map[string][]string)
	for dir, dirStore := range store {
		public[dir] = make(map[string][]string)
		for date, dateStore := range dirStore {
			public[dir][date] = make([]string, 0)
			for tt := range dateStore {
				public[dir][date] = append(public[dir][date], tt)
			}
		}
	}
	return public
}
