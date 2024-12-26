package codemap

import (
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGenerateJavaOutput(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		maxLen   int
		expected []FileCodeMap
	}{
		{
			name:   "Comprehensive string literal truncation test",
			maxLen: 10,
			files: map[string]string{
				"Constants.java": `
package com.example;

public class Constants {
    // Single constant declaration
    public static final String SINGLE_CONST = "This is a long single constant";

    // Grouped constant declaration
    public static final String GROUPED_CONST1 = "First grouped constant";
    public static final String GROUPED_CONST2 = "Second grouped constant";
    public static final String GROUPED_CONST3 = """
        Third grouped constant
        with multiple lines
        """;

    // Constants with different types
    public static final int INT_CONST = 42;
    public static final double FLOAT_CONST = 3.14;
    public static final boolean BOOL_CONST = true;
    public static final char CHAR_CONST = 'A';
    public static final String STRING_CONST = "Regular string constant";

    // Byte array constants
    public static final byte[] BYTE_ARRAY_CONST1 = "Byte array constant".getBytes();
    public static final byte[] BYTE_ARRAY_CONST2 = """
        Raw byte array constant
        """.getBytes();

    // Constant expressions
    public static final int CONST_EXPR1 = "Hello, World!".length();
    public static final int CONST_EXPR2 = 60 * 60 * 24; // Seconds in a day

    private Constants() {
        // Private constructor to prevent instantiation
    }
}
`,
			},
			expected: []FileCodeMap{
				{
					Path: "Constants.java",
					Content: `package com.example;

public class Constants {
    // Single constant declaration
    public static final String SINGLE_CONST = "This is a ...";

    // Grouped constant declaration
    public static final String GROUPED_CONST1 = "First grou...";
    public static final String GROUPED_CONST2 = "Second gro...";
    public static final String GROUPED_CONST3 = """
        T...""";

    // Constants with different types
    public static final int INT_CONST = 42;
    public static final double FLOAT_CONST = 3.14;
    public static final boolean BOOL_CONST = true;
    public static final char CHAR_CONST = 'A';
    public static final String STRING_CONST = "Regular st...";

    // Byte array constants
    public static final byte[] BYTE_ARRAY_CONST1 = "Byte array...".getBytes();
    public static final byte[] BYTE_ARRAY_CONST2 = """
        R...""".getBytes();

    // Constant expressions
    public static final int CONST_EXPR1 = "Hello, Wor...".length();
    public static final int CONST_EXPR2 = 60 * 60 * 24; // Seconds in a day

    private Constants()
}`,
				},
			},
		},
		{
			name:   "Basic Java class",
			maxLen: 50,
			files: map[string]string{
				"Main.java": `
package com.example;

import java.util.List;
import java.util.ArrayList;

public class Main {
    private static final String GREETING = "Hello, World!";
    private List<String> items;

    public Main() {
        items = new ArrayList<>();
    }

    public void addItem(String item) {
        items.add(item);
    }

    public static void main(String[] args) {
        System.out.println(GREETING);
    }
}
`,
			},
			expected: []FileCodeMap{
				{
					Path: "Main.java",
					Content: `package com.example;

import java.util.List;
import java.util.ArrayList;

public class Main {
    private static final String GREETING = "Hello, World!";
    private List<String> items;

    public Main()

    public void addItem(String item)

    public static void main(String[] args)
}`,
				},
			},
		},
		{
			name:   "Java interface and implementation",
			maxLen: 50,
			files: map[string]string{
				"Shape.java": `
package com.example.shapes;

public interface Shape {
    double getArea();
    double getPerimeter();
}
`,
				"Circle.java": `
package com.example.shapes;

public class Circle implements Shape {
    private double radius;

    public Circle(double radius) {
        this.radius = radius;
    }

    @Override
    public double getArea() {
        return Math.PI * radius * radius;
    }

    @Override
    public double getPerimeter() {
        return 2 * Math.PI * radius;
    }
}
`,
			},
			expected: []FileCodeMap{
				{
					Path: "Circle.java",
					Content: `package com.example.shapes;

public class Circle implements Shape {
    private double radius;

    public Circle(double radius)

    @Override
    public double getArea()

    @Override
    public double getPerimeter()
}`,
				},
				{
					Path: "Shape.java",
					Content: `package com.example.shapes;

public interface Shape {
    double getArea();
    double getPerimeter();
}`,
				},
			},
		},
		{
			name:   "Java class with comments and annotations",
			maxLen: 50,
			files: map[string]string{
				"User.java": `
package com.example.models;

import java.util.Date;

/**
 * Represents a user in the system.
 */
public class User {
    private long id;
    private String username;
    private String email;
    private Date createdAt;

    // Constructor
    public User(String username, String email) {
        this.username = username;
        this.email = email;
        this.createdAt = new Date();
    }

    // Getters and setters
    @Deprecated
    public long getId() {
        return id;
    }

    public void setId(long id) {
        this.id = id;
    }

    public String getUsername() {
        return username;
    }

    public void setUsername(String username) {
        this.username = username;
    }

    @Override
    public String toString() {
        return "User{" +
                "id=" + id +
                ", username='" + username + '\'' +
                ", email='" + email + '\'' +
                ", createdAt=" + createdAt +
                '}';
    }
}
`,
			},
			expected: []FileCodeMap{
				{
					Path: "User.java",
					Content: `package com.example.models;

import java.util.Date;

/**
 * Represents a user in the system.
 */
public class User {
    private long id;
    private String username;
    private String email;
    private Date createdAt;

    // Constructor
    public User(String username, String email)

    // Getters and setters
    @Deprecated
    public long getId()

    public void setId(long id)

    public String getUsername()

    public void setUsername(String username)

    @Override
    public String toString()
}`,
				},
			},
		},
		{
			name:   "Java enum and nested class",
			maxLen: 50,
			files: map[string]string{
				"Vehicle.java": `
package com.example.vehicles;

public class Vehicle {
    private String make;
    private String model;
    private VehicleType type;

    public enum VehicleType {
        CAR, TRUCK, MOTORCYCLE, BICYCLE
    }

    public Vehicle(String make, String model, VehicleType type) {
        this.make = make;
        this.model = model;
        this.type = type;
    }

    public static class Engine {
        private int horsepower;

        public Engine(int horsepower) {
            this.horsepower = horsepower;
        }

        public int getHorsepower() {
            return horsepower;
        }
    }

    // Other methods...
}
`,
			},
			expected: []FileCodeMap{
				{
					Path: "Vehicle.java",
					Content: `package com.example.vehicles;

public class Vehicle {
    private String make;
    private String model;
    private VehicleType type;

    public enum VehicleType {
        CAR, TRUCK, MOTORCYCLE, BICYCLE
    }

    public Vehicle(String make, String model, VehicleType type)

    public static class Engine {
        private int horsepower;

        public Engine(int horsepower)

        public int getHorsepower()
    }

    // Other methods...
}`,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the in-memory file system
			memFS := setupInMemoryFS(tt.files)

			// Generate the output using GenerateOutput
			ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
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
