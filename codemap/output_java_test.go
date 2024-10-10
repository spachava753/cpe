package codemap

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGenerateJavaOutput(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		maxLen   int
		expected string
	}{
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
			expected: `<code_map>
<file>
<path>Main.java</path>
<file_map>
package com.example;

import java.util.List;
import java.util.ArrayList;

public class Main {
    private static final String GREETING = "Hello, World!";
    private List<String> items;

    public Main()

    public void addItem(String item)

    public static void main(String[] args)
}
</file_map>
</file>
</code_map>
`,
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
			expected: `<code_map>
<file>
<path>Circle.java</path>
<file_map>
package com.example.shapes;

public class Circle implements Shape {
    private double radius;

    public Circle(double radius)

    @Override
    public double getArea()

    @Override
    public double getPerimeter()
}
</file_map>
</file>
<file>
<path>Shape.java</path>
<file_map>
package com.example.shapes;

public interface Shape {
    double getArea();
    double getPerimeter();
}
</file_map>
</file>
</code_map>
`,
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
			expected: `<code_map>
<file>
<path>User.java</path>
<file_map>
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
    public User(String username, String email)

    // Getters and setters
    @Deprecated
    public long getId()

    public void setId(long id)

    public String getUsername()

    public void setUsername(String username)

    @Override
    public String toString()
}
</file_map>
</file>
</code_map>
`,
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
			expected: `<code_map>
<file>
<path>Vehicle.java</path>
<file_map>
package com.example.vehicles;

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
}
</file_map>
</file>
</code_map>
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the in-memory file system
			memFS := setupInMemoryFS(tt.files)

			// Generate the output using GenerateOutput
			output, err := GenerateOutput(memFS, tt.maxLen)
			if err != nil {
				t.Fatalf("Failed to generate output: %v", err)
			}

			// Compare the output with the expected result
			if !assert.Equal(t, tt.expected, output) {
				t.Errorf("Unexpected output.\nExpected:\n%s\nGot:\n%s", tt.expected, output)
			}
		})
	}
}
