// Package fixture builds a deterministic core.Project shared by renderer
// golden tests.
package fixture

import "codetree/core"

// Project returns the "zoo" sample matching the README example.
func Project() *core.Project {
	return &core.Project{
		Root: "/proj/myproject",
		Files: []*core.File{
			{
				Path: "models/animal.py", Lang: "python",
				Symbols: []*core.Symbol{
					{Name: "Animal", Kind: core.KindClass, File: "models/animal.py", Line: 1, Doc: "Base animal.",
						Children: []*core.Symbol{
							{Name: "speak", Kind: core.KindMethod, Detail: "(self)", File: "models/animal.py", Line: 2},
						}},
					{Name: "Dog", Kind: core.KindClass, Detail: "(Animal)", File: "models/animal.py", Line: 5, SuperTypes: []string{"Animal"},
						Children: []*core.Symbol{
							{Name: "speak", Kind: core.KindMethod, Detail: "(self)", File: "models/animal.py", Line: 6},
							{Name: "fetch", Kind: core.KindMethod, Detail: "(self)", File: "models/animal.py", Line: 9},
						}},
				},
			},
			{
				Path: "models/zoo.py", Lang: "python",
				Symbols: []*core.Symbol{
					{Name: "make_sound", Kind: core.KindFunction, Detail: "(a)", File: "models/zoo.py", Line: 1},
				},
			},
		},
	}
}
