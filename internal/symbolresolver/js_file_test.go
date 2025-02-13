package symbolresolver

import (
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/stretchr/testify/assert"
	"io/fs"
	"testing/fstest"
)

func TestResolveJavaScriptFiles(t *testing.T) {
	// Helper function to create an in-memory file system
	createTestFS := func(files map[string]string) fs.FS {
		fsys := fstest.MapFS{}
		for path, content := range files {
			fsys[path] = &fstest.MapFile{Data: []byte(content)}
		}
		return fsys
	}

	// Test case 1: Single input file with ES6 imports
	t.Run("SingleInputES6Imports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/math.js": `
export const PI = 3.14159;
export function square(x) { return x * x; }
export class Circle {
    constructor(radius) {
        this.radius = radius;
    }
}`,
			"utils/extra.js": `
// This file should not be included in results
export function unused() {}`,
			"main.js": `
import { PI, square, Circle } from './utils/math';

function calculateArea(radius) {
    const circle = new Circle(radius);
    return PI * square(circle.radius);
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"main.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"utils/math.js": true,
			"main.js":       true,
		}, result)
	})

	// Test case 2: Multiple input files with ES6 imports
	t.Run("MultipleInputsES6Imports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/strings.js": `
export function capitalize(str) {
    return str.charAt(0).toUpperCase() + str.slice(1);
}`,
			"utils/numbers.js": `
export function sum(arr) {
    return arr.reduce((a, b) => a + b, 0);
}`,
			"process.js": `
import { capitalize } from './utils/strings';
export function processName(name) {
    return capitalize(name);
}`,
			"calculate.js": `
import { sum } from './utils/numbers';
export function getTotal(items) {
    return sum(items.map(i => i.value));
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"process.js", "calculate.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"utils/strings.js": true,
			"utils/numbers.js": true,
			"process.js":      true,
			"calculate.js":    true,
		}, result)
	})

	// Test case 3: Single input file with CommonJS requires
	t.Run("SingleInputCommonJSRequires", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"config/database.js": `
const config = {
    host: 'localhost',
    port: 5432,
};
module.exports = config;`,
			"config/cache.js": `
// This file should not be included
module.exports = { enabled: true };`,
			"db.js": `
const dbConfig = require('./config/database');
console.log("Connecting to", dbConfig.host);`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"db.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"config/database.js": true,
			"db.js":             true,
		}, result)
	})

	// Test case 4: Multiple input files with mixed module systems
	t.Run("MultipleInputsMixedModules", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/logger.js": `
module.exports = function log(msg) {
    console.log(msg);
};`,
			"utils/format.js": `
export function formatDate(date) {
    return date.toISOString();
}`,
			"service1.js": `
const log = require('./utils/logger');
log('Service 1 starting...');`,
			"service2.js": `
import { formatDate } from './utils/format';
console.log('Started at:', formatDate(new Date()));`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"service1.js", "service2.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"utils/logger.js": true,
			"utils/format.js": true,
			"service1.js":    true,
			"service2.js":    true,
		}, result)
	})

	// Test case 5: Object destructuring in requires
	t.Run("ObjectDestructuringRequires", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"api/handlers.js": `
exports.userHandler = function(req, res) {};
exports.adminHandler = function(req, res) {};`,
			"middleware/auth.js": `
module.exports.authenticate = function(req, res, next) {};`,
			"app.js": `
const { userHandler, adminHandler } = require('./api/handlers');
const { authenticate } = require('./middleware/auth');

app.get('/user', authenticate, userHandler);`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"app.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"api/handlers.js":    true,
			"middleware/auth.js": true,
			"app.js":            true,
		}, result)
	})

	// Test case 6: Multiple files with namespace imports
	t.Run("MultipleFilesNamespaceImports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/strings.js": `
export function capitalize(str) {
    return str.charAt(0).toUpperCase() + str.slice(1);
}`,
			"utils/numbers.js": `
export const add = (a, b) => a + b;
export const multiply = (a, b) => a * b;`,
			"service1.js": `
import * as str from './utils/strings';
console.log(str.capitalize('hello'));`,
			"service2.js": `
import * as math from './utils/numbers';
console.log(math.add(1, 2));`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"service1.js", "service2.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"utils/strings.js": true,
			"utils/numbers.js": true,
			"service1.js":     true,
			"service2.js":     true,
		}, result)
	})

	// Test case 7: Dynamic imports (should only resolve static requires/imports)
	t.Run("DynamicImports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"plugins/feature1.js": `
export function feature1() {
    return 'feature1';
}`,
			"plugins/feature2.js": `
module.exports = function feature2() {
    return 'feature2';
};`,
			"loader.js": `
// Dynamic import should not be resolved
async function loadFeature(name) {
    const module = await import('./plugins/' + name);
    return module.default || module;
}
// Static require should be resolved
const feature2 = require('./plugins/feature2');`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"loader.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"plugins/feature2.js": true,
			"loader.js":          true,
		}, result)
	})

	// Test case 8: Multiple files with re-exports (should only resolve direct imports)
	t.Run("MultipleFilesReExports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"models/user.js": `
export class User {
    constructor(name) { this.name = name; }
}`,
			"models/index.js": `
export * from './user';`,
			"service1.js": `
import { User } from './models/index';
const user = new User('John');`,
			"service2.js": `
import { User } from './models';
const user = new User('Jane');`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"service1.js", "service2.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"models/index.js": true,
			"service1.js":    true,
			"service2.js":    true,
		}, result)
	})

	// Test case 9: Multiple files with circular dependencies
	t.Run("CircularDependencies", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"a.js": `
import { b } from './b';
export const a = () => b() + 1;`,
			"b.js": `
import { a } from './a';
export const b = () => a() + 1;`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"a.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"a.js": true,
			"b.js": true,
		}, result)
	})
}