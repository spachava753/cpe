package symbolresolver

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestExtractJavaScriptSymbols(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		want     []string
		wantErr  bool
		wantLike []string // partial matches for query strings
	}{
		{
			name: "ES6 named exports",
			source: `export const PI = 3.14159;
export function square(x) { return x * x; }
export class Circle {
    constructor(radius) {
        this.radius = radius;
    }
}
export { PI, square, Circle };`,
			wantLike: []string{
				"PI",
				"square",
				"Circle",
			},
		},
		{
			name: "ES6 default export",
			source: `class Calculator {
    add(a, b) { return a + b; }
    subtract(a, b) { return a - b; }
}
export default Calculator;`,
			wantLike: []string{
				"Calculator",
			},
		},
		{
			name: "CommonJS exports",
			source: `const config = {
    apiKey: 'secret',
    timeout: 1000
};
function connect() {}
class Database {}

module.exports = {
    config,
    connect,
    Database
};`,
			wantLike: []string{
				"config",
				"connect",
				"Database",
			},
		},
		{
			name: "CommonJS exports.property",
			source: `exports.PI = 3.14159;
exports.square = function(x) { return x * x; };
exports.Circle = class {
    constructor(radius) {
        this.radius = radius;
    }
};`,
			wantLike: []string{
				"PI",
				"square",
				"Circle",
			},
		},
		{
			name: "Mixed export styles",
			source: `// ES6 exports
export const VERSION = '1.0.0';
export class Logger {}

// CommonJS exports
module.exports.config = {
    debug: true
};
exports.helper = function() {};`,
			wantLike: []string{
				"VERSION",
				"Logger",
				"config",
				"helper",
			},
		},
		{
			name: "ES6 import usage",
			source: `import { useState, useEffect } from 'react';
import axios from 'axios';
import * as utils from './utils';

function MyComponent() {
    const [data, setData] = useState(null);
    useEffect(() => {
        axios.get('/api/data').then(setData);
    }, []);
    return utils.formatData(data);
}`,
			wantLike: []string{
				"useState",
				"useEffect",
				"axios",
				"utils",
			},
		},
		{
			name: "CommonJS require usage",
			source: `const { join } = require('path');
const express = require('express');
const { logger } = require('./utils');

const app = express();
app.get('/', (req, res) => {
    logger.info(join(__dirname, 'index.html'));
    res.send('Hello');
});`,
			wantLike: []string{
				"join",
				"express",
				"logger",
			},
		},
		{
			name: "Object destructuring usage",
			source: `const { name, age } = person;
const { x, y, ...rest } = coordinates;
function process({ id, timestamp }) {
    console.log(id, timestamp);
}`,
			wantLike: []string{
				"person",
				"coordinates",
				"id",
				"timestamp",
			},
		},
		{
			name: "Class and method usage",
			source: `class UserService {
    constructor(repo) {
        this.repo = repo;
    }
    async findById(id) {
        return this.repo.find(id);
    }
}

const service = new UserService(repo);
const user = await service.findById(123);`,
			wantLike: []string{
				"UserService",
				"repo",
				"find",
			},
		},
		{
			name: "Function and variable usage",
			source: `function calculateTotal(items) {
    return items.reduce((sum, item) => sum + item.price, 0);
}

const items = getItems();
const total = calculateTotal(items);
processOrder({ items, total });`,
			wantLike: []string{
				"getItems",
				"calculateTotal",
				"processOrder",
			},
		},
	}

	parser := sitter.NewParser()
	defer parser.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries, err := extractJavaScriptSymbols([]byte(tt.source), parser)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// If exact matches are specified
			if tt.want != nil {
				assert.ElementsMatch(t, tt.want, queries)
			}

			// If partial matches are specified
			if tt.wantLike != nil {
				for _, symbol := range tt.wantLike {
					found := false
					for _, query := range queries {
						if strings.Contains(query, symbol) {
							found = true
							break
						}
					}
					assert.True(t, found, "Symbol %q not found in queries", symbol)
				}
			}
		})
	}
}