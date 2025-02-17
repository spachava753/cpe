package codemap

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGenerateJavaScriptOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name: "function declarations",
			input: `function add(a, b) {
    return a + b;
}

function multiply(x, y) {
    const result = x * y;
    return result;
}`,
			want: `function add(a, b)

function multiply(x, y)`,
		},
		{
			name: "class declarations",
			input: `class Animal {
    constructor(name) {
        this.name = name;
    }

    speak() {
        console.log(this.name + ' makes a sound.');
    }
}

class Dog extends Animal {
    constructor(name) {
        super(name);
    }

    speak() {
        console.log(this.name + ' barks.');
    }
}`,
			want: `class Animal {
    constructor(name)

    speak()
}

class Dog extends Animal {
    constructor(name)

    speak()
}`,
		},
		{
			name: "ES6 module syntax",
			input: `import { useState, useEffect } from 'react';
import axios from 'axios';

export const fetchData = async () => {
    const response = await axios.get('/api/data');
    return response.data;
};

export default function DataComponent() {
    const [data, setData] = useState(null);

    useEffect(() => {
        fetchData().then(setData);
    }, []);

    return data ? <div>{data}</div> : <div>Loading...</div>;
}`,
			want: `import { useState, useEffect } from 'react';
import axios from 'axios';

export const fetchData = async () => {
    const response = await axios.get('/api/data');
    return response.data;
};

export default function DataComponent()`,
		},
		{
			name: "CommonJS module syntax",
			input: `const fs = require('fs');
const path = require('path');

function readConfigFile(filePath) {
    const fullPath = path.resolve(filePath);
    return fs.readFileSync(fullPath, 'utf8');
}

class ConfigParser {
    constructor(config) {
        this.config = JSON.parse(config);
    }

    getValue(key) {
        return this.config[key];
    }
}

module.exports = {
    readConfigFile,
    ConfigParser
};`,
			want: `const fs = require('fs');
const path = require('path');

function readConfigFile(filePath)

class ConfigParser {
    constructor(config)

    getValue(key)
}

module.exports = {
    readConfigFile,
    ConfigParser
};`,
		},
		{
			name: "arrow functions and object methods",
			input: `const utils = {
    add: (a, b) => {
        return a + b;
    },
    
    multiply(x, y) {
        return x * y;
    },
    
    divide: function(a, b) {
        if (b === 0) throw new Error('Division by zero');
        return a / b;
    }
};

const process = async (data) => {
    const result = await someAsyncOperation(data);
    return result;
};`,
			want: `const utils = {
    add: (a, b) => {
        return a + b;
    },

    multiply(x, y) ,

    divide: function(a, b) {
        if (b === 0) throw new Error('Division by zero');
        return a / b;
    }
};

const process = async (data) => {
    const result = await someAsyncOperation(data);
    return result;
};`,
		},
		{
			name: "string literals",
			input: `const shortString = "This is fine";
const longString = "This is a very long string that should be truncated because it exceeds the maximum length limit for string literals in our code map output generation process";
const template = ` + "`" + `This is a template
string with multiple
lines that should
be truncated` + "`" + `;`,
			want: `const shortString = "This is fine";
const longString = "This is a very long ...";
const template = ` + "`" + `This is a template
s...` + "`" + `;`,
		},
		{
			name: "IIFE and closures",
			input: `(function() {
    let counter = 0;
    
    function increment() {
        counter++;
        return counter;
    }
    
    window.increment = increment;
})();

const counter = (function() {
    let count = 0;
    return {
        increment() {
            return ++count;
        },
        decrement() {
            return --count;
        }
    };
})();`,
			want: `(function() {
    let counter = 0;

    function increment()

    window.increment = increment;
})();

const counter = (function() {
    let count = 0;
    return {
        increment()

        decrement()

    };
})();`,
		},
		{
			name: "async/await and generators",
			input: `async function fetchUser(id) {
    const response = await fetch('/api/users/' + id);
    const data = await response.json();
    return data;
}

function* numberGenerator() {
    yield 1;
    yield 2;
    yield 3;
}

async function* asyncGenerator() {
    yield await Promise.resolve(1);
    yield await Promise.resolve(2);
    yield await Promise.resolve(3);
}`,
			want: `async function fetchUser(id)

function* numberGenerator()

async function* asyncGenerator()`,
		},
		{
			name: "decorators and class fields",
			input: `const log = (target, name, descriptor) => {
    // decorator implementation
};

class Example {
    static count = 0;
    
    @log
    method() {
        console.log('method called');
    }
    
    @log
    static staticMethod() {
        console.log('static method called');
    }
}`,
			want: `const log = (target, name, descriptor) => {
    // decorator implementation
};

class Example {
    static count = 0;

    @log
    method()

    @log
    static staticMethod()
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateJavaScriptFileOutput([]byte(tt.input), 20)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}