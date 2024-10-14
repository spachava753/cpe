package codemap

import (
	"github.com/spachava753/cpe/ignore"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGeneratePythonOutput(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		maxLen   int
		expected []FileCodeMap
	}{
		{
			name:   "Basic Python function and class",
			maxLen: 50,
			files: map[string]string{
				"main.py": `
def greet(name):
    return f"Hello, {name}!"

class Person:
    def __init__(self, name, age):
        self.name = name
        self.age = age

    def introduce(self):
        return f"My name is {self.name} and I'm {self.age} years old."

if __name__ == "__main__":
    person = Person("Alice", 30)
    print(greet(person.name))
    print(person.introduce())
`,
			},
			expected: []FileCodeMap{
				{
					Path: "main.py",
					Content: `<file>
<path>main.py</path>
<file_map>
def greet(name):
    pass

class Person:
    def __init__(self, name, age):
        pass

    def introduce(self):
        pass

if __name__ == "__main__":
    person = Person("Alice", 30)
    print(greet(person.name))
    print(person.introduce())
</file_map>
</file>
`,
				},
			},
		},
		{
			name:   "Python class with decorators and docstrings",
			maxLen: 50,
			files: map[string]string{
				"user.py": `
import datetime

class User:
    """
    Represents a user in the system.
    """

    def __init__(self, username, email):
        self.username = username
        self.email = email
        self.created_at = datetime.datetime.now()

    @property
    def display_name(self):
        """
        Returns the user's display name.
        """
        return self.username.capitalize()

    @classmethod
    def from_dict(cls, data):
        """
        Creates a User instance from a dictionary.
        """
        return cls(data['username'], data['email'])

    def __str__(self):
        return f"User(username={self.username}, email={self.email})"

    def __repr__(self):
        return self.__str__()
`,
			},
			expected: []FileCodeMap{
				{
					Path: "user.py",
					Content: `<file>
<path>user.py</path>
<file_map>
import datetime

class User:
    """
    Represents a user in the system.
    """

    def __init__(self, username, email):
        pass

    @property
    def display_name(self):
        """
        Returns the user's display name.
        """
        pass

    @classmethod
    def from_dict(cls, data):
        """
        Creates a User instance from a dictionary.
        """
        pass

    def __str__(self):
        pass

    def __repr__(self):
        pass
</file_map>
</file>
`,
				},
			},
		},
		{
			name:   "Python with multiple classes and functions",
			maxLen: 50,
			files: map[string]string{
				"shapes.py": `
import math

class Shape:
    def area(self):
        raise NotImplementedError("Subclass must implement abstract method")

    def perimeter(self):
        raise NotImplementedError("Subclass must implement abstract method")

class Circle(Shape):
    def __init__(self, radius):
        self.radius = radius

    def area(self):
        return math.pi * self.radius ** 2

    def perimeter(self):
        return 2 * math.pi * self.radius

class Rectangle(Shape):
    def __init__(self, width, height):
        self.width = width
        self.height = height

    def area(self):
        return self.width * self.height

    def perimeter(self):
        return 2 * (self.width + self.height)

def calculate_total_area(shapes):
    return sum(shape.area() for shape in shapes)

if __name__ == "__main__":
    circle = Circle(5)
    rectangle = Rectangle(4, 6)
    print(f"Circle area: {circle.area()}")
    print(f"Rectangle perimeter: {rectangle.perimeter()}")
    print(f"Total area: {calculate_total_area([circle, rectangle])}")
`,
			},
			expected: []FileCodeMap{
				{
					Path: "shapes.py",
					Content: `<file>
<path>shapes.py</path>
<file_map>
import math

class Shape:
    def area(self):
        pass

    def perimeter(self):
        pass

class Circle(Shape):
    def __init__(self, radius):
        pass

    def area(self):
        pass

    def perimeter(self):
        pass

class Rectangle(Shape):
    def __init__(self, width, height):
        pass

    def area(self):
        pass

    def perimeter(self):
        pass

def calculate_total_area(shapes):
    pass

if __name__ == "__main__":
    circle = Circle(5)
    rectangle = Rectangle(4, 6)
    print(f"Circle area: {circle.area()}")
    print(f"Rectangle perimeter: {rectangle.perimeter()}")
    print(f"Total area: {calculate_total_area([circle, rectangle])}")
</file_map>
</file>
`,
				},
			},
		},
		{
			name:   "Python with nested functions and lambda",
			maxLen: 50,
			files: map[string]string{
				"nested.py": `
def outer_function(x):
    def inner_function(y):
        return x + y
    return inner_function

lambda_func = lambda x: x * 2

def higher_order_function(func, value):
    return func(value)

if __name__ == "__main__":
    closure = outer_function(10)
    print(closure(5))
    print(lambda_func(3))
    print(higher_order_function(lambda x: x ** 2, 4))
`,
			},
			expected: []FileCodeMap{
				{
					Path: "nested.py",
					Content: `<file>
<path>nested.py</path>
<file_map>
def outer_function(x):
    pass

lambda_func = lambda x: x * 2

def higher_order_function(func, value):
    pass

if __name__ == "__main__":
    closure = outer_function(10)
    print(closure(5))
    print(lambda_func(3))
    print(higher_order_function(lambda x: x ** 2, 4))
</file_map>
</file>
`,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the in-memory file system
			memFS := setupInMemoryFS(tt.files)

			// Generate the output using GenerateOutput
			ignoreRules := ignore.NewIgnoreRules()
			output, err := GenerateOutput(memFS, tt.maxLen, ignoreRules)
			if err != nil {
				t.Fatalf("Failed to generate output: %v", err)
			}

			// Compare the output with the expected result
			if !assert.Equal(t, tt.expected, output) {
				t.Errorf("Unexpected output.\nExpected:\n%v\nGot:\n%v", tt.expected, output)
			}
		})
	}
}
