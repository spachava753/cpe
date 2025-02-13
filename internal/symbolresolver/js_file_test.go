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

	// Test case 1: ES6 named exports and imports
	t.Run("ES6NamedExportsAndImports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/math.js": `
export const PI = 3.14159;
export function square(x) { return x * x; }
export class Circle {
    constructor(radius) {
        this.radius = radius;
    }
}`,
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

	// Test case 2: ES6 default export and import
	t.Run("ES6DefaultExportAndImport", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"models/user.js": `
class User {
    constructor(name) {
        this.name = name;
    }
    greet() {
        return "Hello, " + this.name;
    }
}
export default User;`,
			"services/userService.js": `
import User from '../models/user';

export function createUser(name) {
    return new User(name);
}`,
			"main.js": `
import { createUser } from './services/userService';

const user = createUser("John");
console.log(user.greet());`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"main.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"models/user.js":         true,
			"services/userService.js": true,
			"main.js":                true,
		}, result)
	})

	// Test case 3: CommonJS exports and requires
	t.Run("CommonJSExportsAndRequires", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"config/database.js": `
const config = {
    host: 'localhost',
    port: 5432,
    user: 'admin'
};

module.exports = config;`,
			"db/connection.js": `
const config = require('../config/database');

function connect() {
    console.log("Connecting to", config.host);
}

module.exports = { connect };`,
			"server.js": `
const { connect } = require('./db/connection');
connect();`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"server.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"config/database.js": true,
			"db/connection.js":   true,
			"server.js":          true,
		}, result)
	})

	// Test case 4: Mixed module systems
	t.Run("MixedModuleSystems", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"lib/logger.js": `
class Logger {
    log(msg) {
        console.log(msg);
    }
}
module.exports = Logger;`,
			"utils/format.js": `
export function formatDate(date) {
    return date.toISOString();
}`,
			"main.js": `
import { formatDate } from './utils/format';
const Logger = require('./lib/logger');

const logger = new Logger();
logger.log(formatDate(new Date()));`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"main.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"lib/logger.js":   true,
			"utils/format.js": true,
			"main.js":         true,
		}, result)
	})

	// Test case 5: Re-exports and namespace imports
	t.Run("ReExportsAndNamespaceImports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/strings.js": `
export function capitalize(str) {
    return str.charAt(0).toUpperCase() + str.slice(1);
}
export function reverse(str) {
    return str.split('').reverse().join('');
}`,
			"utils/index.js": `
export * from './strings';
export * as numbers from './math';`,
			"utils/math.js": `
export const add = (a, b) => a + b;
export const multiply = (a, b) => a * b;`,
			"main.js": `
import * as utils from './utils';
import { capitalize } from './utils/strings';

console.log(utils.numbers.add(1, 2));
console.log(capitalize('hello'));`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"main.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"utils/strings.js": true,
			"utils/index.js":   true,
			"utils/math.js":    true,
			"main.js":          true,
		}, result)
	})

	// Test case 6: Object destructuring and property exports
	t.Run("ObjectDestructuringAndPropertyExports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"api/handlers.js": `
exports.userHandler = function(req, res) {
    res.send('user');
};
exports.adminHandler = function(req, res) {
    res.send('admin');
};`,
			"middleware/auth.js": `
module.exports.authenticate = function(req, res, next) {
    next();
};`,
			"app.js": `
const { userHandler, adminHandler } = require('./api/handlers');
const { authenticate } = require('./middleware/auth');

app.get('/user', authenticate, userHandler);
app.get('/admin', authenticate, adminHandler);`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"app.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"api/handlers.js":     true,
			"middleware/auth.js":  true,
			"app.js":             true,
		}, result)
	})

	// Test case 7: Class inheritance and module dependencies
	t.Run("ClassInheritanceAndModuleDependencies", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"models/base.js": `
export class BaseModel {
    constructor(id) {
        this.id = id;
    }
    save() {
        console.log('saving...');
    }
}`,
			"models/user.js": `
import { BaseModel } from './base';
import { validateEmail } from '../utils/validators';

export class User extends BaseModel {
    constructor(id, email) {
        super(id);
        if (!validateEmail(email)) {
            throw new Error('Invalid email');
        }
        this.email = email;
    }
}`,
			"utils/validators.js": `
export function validateEmail(email) {
    return email.includes('@');
}`,
			"main.js": `
import { User } from './models/user';
const user = new User(1, 'test@example.com');
user.save();`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"main.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"models/base.js":       true,
			"models/user.js":       true,
			"utils/validators.js":  true,
			"main.js":             true,
		}, result)
	})

	// Test case 8: Dynamic imports and exports
	t.Run("DynamicImportsAndExports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"plugins/feature1.js": `
export function feature1() {
    return 'feature1';
}`,
			"plugins/feature2.js": `
module.exports = function feature2() {
    return 'feature2';
};`,
			"main.js": `
async function loadFeature(name) {
    const module = await import('./plugins/' + name);
    return module.default || module;
}
const feature2 = require('./plugins/feature2');`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"main.js"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"plugins/feature2.js": true,
			"main.js":            true,
		}, result)
	})
}