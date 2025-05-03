package codemap

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGenerateTypeScriptOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name: "interfaces",
			input: `interface User {
    id: number;
    name: string;
    email?: string;
    readonly createdAt: Date;
    getFullName(): string;
}

interface AdminUser extends User {
    role: 'admin';
    permissions: string[];
}`,
			want: `interface User {
    id: number;
    name: string;
    email?: string;
    readonly createdAt: Date;
    getFullName(): string;
}

interface AdminUser extends User {
    role: 'admin';
    permissions: string[];
}`,
		},
		{
			name: "type aliases and unions",
			input: `type ID = string | number;

type Status = 'pending' | 'active' | 'inactive';

type ApiResponse<T> = {
    data: T;
    status: number;
    message: string;
    timestamp: Date;
};

type UserResponse = ApiResponse<{
    id: ID;
    name: string;
    status: Status;
}>;`,
			want: `type ID = string | number;

type Status = 'pending' | 'active' | 'inactive';

type ApiResponse<T> = {
    data: T;
    status: number;
    message: string;
    timestamp: Date;
};

type UserResponse = ApiResponse<{
    id: ID;
    name: string;
    status: Status;
}>;`,
		},
		{
			name: "classes with type annotations",
			input: `class Queue<T> {
    private items: T[] = [];

    constructor(initialItems?: T[]) {
        if (initialItems) {
            this.items = [...initialItems];
        }
    }

    enqueue(item: T): void {
        this.items.push(item);
    }

    dequeue(): T | undefined {
        return this.items.shift();
    }

    peek(): T | undefined {
        return this.items[0];
    }
}`,
			want: `class Queue<T> {
    private items: T[] = [];

    constructor(initialItems?: T[]) 

    enqueue(item: T): void 

    dequeue(): T | undefined 

    peek(): T | undefined 
}`,
		},
		{
			name: "abstract classes and methods",
			input: `abstract class Shape {
    constructor(protected color: string) {}

    abstract getArea(): number;
    abstract getPerimeter(): number;

    getColor(): string {
        return this.color;
    }
}

class Circle extends Shape {
    constructor(
        color: string,
        private radius: number
    ) {
        super(color);
    }

    getArea(): number {
        return Math.PI * this.radius ** 2;
    }

    getPerimeter(): number {
        return 2 * Math.PI * this.radius;
    }
}`,
			want: `abstract class Shape {
    constructor(protected color: string) 

    abstract getArea(): number;
    abstract getPerimeter(): number;

    getColor(): string 
}

class Circle extends Shape {
    constructor(
        color: string,
        private radius: number
    ) 

    getArea(): number 

    getPerimeter(): number 
}`,
		},
		{
			name: "decorators with type information",
			input: `function log(target: any, propertyKey: string, descriptor: PropertyDescriptor) {
    // decorator implementation
}

@injectable()
class UserService {
    constructor(
        @inject('UserRepository')
        private userRepo: UserRepository
    ) {}

    @log
    async getUser(@param('id') id: string): Promise<User> {
        return this.userRepo.findById(id);
    }

    @validate
    createUser(@body() userData: CreateUserDTO): Promise<User> {
        return this.userRepo.create(userData);
    }
}`,
			want: `function log(target: any, propertyKey: string, descriptor: PropertyDescriptor) 

@injectable()
class UserService {
    constructor(
        @inject('UserRepository')
        private userRepo: UserRepository
    ) 

    @log
    async getUser(@param('id') id: string): Promise<User> 

    @validate
    createUser(@body() userData: CreateUserDTO): Promise<User> 
}`,
		},
		{
			name: "generics and constraints",
			input: `interface HasId {
    id: string | number;
}

class Repository<T extends HasId> {
    private items: Map<T['id'], T> = new Map();

    save(item: T): void {
        this.items.set(item.id, item);
    }

    findById(id: T['id']): T | undefined {
        return this.items.get(id);
    }
}

function merge<T extends object, U extends object>(obj1: T, obj2: U): T & U {
    return { ...obj1, ...obj2 };
}`,
			want: `interface HasId {
    id: string | number;
}

class Repository<T extends HasId> {
    private items: Map<T['id'], T> = new Map();

    save(item: T): void 

    findById(id: T['id']): T | undefined 
}

function merge<T extends object, U extends object>(obj1: T, obj2: U): T & U`,
		},
		{
			name: "enums and namespaces",
			input: `enum Direction {
    Up = 'UP',
    Down = 'DOWN',
    Left = 'LEFT',
    Right = 'RIGHT'
}

namespace Validation {
    export interface StringValidator {
        isValid(s: string): boolean;
    }

    export class RegexValidator implements StringValidator {
        constructor(private regex: RegExp) {}

        isValid(s: string): boolean {
            return this.regex.test(s);
        }
    }
}`,
			want: `enum Direction {
    Up = 'UP',
    Down = 'DOWN',
    Left = 'LEFT',
    Right = 'RIGHT'
}

namespace Validation {
    export interface StringValidator {
        isValid(s: string): boolean;
    }

    export class RegexValidator implements StringValidator {
        constructor(private regex: RegExp) 

        isValid(s: string): boolean 
    }
}`,
		},
		{
			name: "utility types and mapped types",
			input: `type Readonly<T> = {
    readonly [P in keyof T]: T[P];
};

type Partial<T> = {
    [P in keyof T]?: T[P];
};

type Pick<T, K extends keyof T> = {
    [P in K]: T[P];
};

type Record<K extends keyof any, T> = {
    [P in K]: T;
};

interface Todo {
    title: string;
    description: string;
    completed: boolean;
}

type ReadonlyTodo = Readonly<Todo>;
type PartialTodo = Partial<Todo>;
type TodoPreview = Pick<Todo, 'title' | 'completed'>;
type TodoRecord = Record<'home' | 'work', Todo>;`,
			want: `type Readonly<T> = {
    readonly [P in keyof T]: T[P];
};

type Partial<T> = {
    [P in keyof T]?: T[P];
};

type Pick<T, K extends keyof T> = {
    [P in K]: T[P];
};

type Record<K extends keyof any, T> = {
    [P in K]: T;
};

interface Todo {
    title: string;
    description: string;
    completed: boolean;
}

type ReadonlyTodo = Readonly<Todo>;
type PartialTodo = Partial<Todo>;
type TodoPreview = Pick<Todo, 'title' | 'completed'>;
type TodoRecord = Record<'home' | 'work', Todo>;`,
		},
		{
			name: "declaration merging",
			input: `interface Box {
    height: number;
    width: number;
}

interface Box {
    scale: number;
}

class Box {
    constructor(public height: number, public width: number, public scale: number) {}

    getArea(): number {
        return this.height * this.width * this.scale;
    }
}

namespace Box {
    export function create(height: number, width: number, scale: number = 1): Box {
        return new Box(height, width, scale);
    }
}`,
			want: `interface Box {
    height: number;
    width: number;
}

interface Box {
    scale: number;
}

class Box {
    constructor(public height: number, public width: number, public scale: number) 

    getArea(): number 
}

namespace Box {
    export function create(height: number, width: number, scale: number = 1): Box 
}`,
		},
		{
			name: "conditional types and infer",
			input: `type IsArray<T> = T extends Array<any> ? true : false;

type UnwrapPromise<T> = T extends Promise<infer U> ? U : T;

type ArrayElement<T> = T extends Array<infer E> ? E : never;

type ReturnType<T extends (...args: any) => any> = T extends (...args: any) => infer R ? R : any;

async function getData(): Promise<string[]> {
    return ['data'];
}

type DataType = UnwrapPromise<ReturnType<typeof getData>>;  // string[]
type ElementType = ArrayElement<DataType>;  // string`,
			want: `type IsArray<T> = T extends Array<any> ? true : false;

type UnwrapPromise<T> = T extends Promise<infer U> ? U : T;

type ArrayElement<T> = T extends Array<infer E> ? E : never;

type ReturnType<T extends (...args: any) => any> = T extends (...args: any) => infer R ? R : any;

async function getData(): Promise<string[]> 

type DataType = UnwrapPromise<ReturnType<typeof getData>>;  // string[]
type ElementType = ArrayElement<DataType>;  // string`,
		},
		{
			name: "stagehand file",
			input: `import { test, expect } from "@playwright/test";
import { Stagehand } from "@/dist";
import StagehandConfig from "@/evals/deterministic/stagehand.config";

test.describe("StagehandContext - addInitScript", () => {
  test("should inject a script on the context before pages load", async () => {
    const stagehand = new Stagehand(StagehandConfig);
    await stagehand.init();

    const context = stagehand.context;

    await context.addInitScript(() => {
      const w = window as typeof window & {
        __testContextScriptVar?: string;
      };
      w.__testContextScriptVar = "Hello from context.initScript!";
    });

    const pageA = await context.newPage();
    await pageA.goto("https://example.com");

    const resultA = await pageA.evaluate(() => {
      const w = window as typeof window & {
        __testContextScriptVar?: string;
      };
      return w.__testContextScriptVar;
    });
    expect(resultA).toBe("Hello from context.initScript!");

    const pageB = await context.newPage();
    await pageB.goto("https://docs.browserbase.com");

    const resultB = await pageB.evaluate(() => {
      const w = window as typeof window & {
        __testContextScriptVar?: string;
      };
      return w.__testContextScriptVar;
    });
    expect(resultB).toBe("Hello from context.initScript!");

    await stagehand.close();
  });
});
`,
			want: `import { test, expect } from "@playwright/test";
import { Stagehand } from "@/dist";
import StagehandConfig from "@/evals/deterministi...";

test.describe("StagehandContext - a...", () => );`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateTypeScriptFileOutput([]byte(tt.input), 20)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
