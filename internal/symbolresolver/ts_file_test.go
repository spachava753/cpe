package symbolresolver

import (
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/stretchr/testify/assert"
	"io/fs"
	"testing/fstest"
)

func TestResolveTypeScriptFiles(t *testing.T) {
	// Helper function to create an in-memory file system
	createTestFS := func(files map[string]string) fs.FS {
		fsys := fstest.MapFS{}
		for path, content := range files {
			fsys[path] = &fstest.MapFile{Data: []byte(content)}
		}
		return fsys
	}

	// Test case 1: Basic TypeScript features
	t.Run("BasicTypeScriptFeatures", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"types/user.ts": `
interface User {
    id: number;
    name: string;
    email: string;
}

type UserRole = 'admin' | 'user' | 'guest';

export class UserService {
    constructor(private readonly user: User) {}
    
    getRole(): UserRole {
        return 'user';
    }
}`,
			"services/auth.ts": `
import { UserService } from '../types/user';

export class AuthService {
    constructor(private userService: UserService) {}
    
    validateUser(): boolean {
        return this.userService.getRole() === 'admin';
    }
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"services/auth.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"types/user.ts":     true,
			"services/auth.ts":  true,
		}, result)
	})

	// Test case 2: Generic classes and interfaces
	t.Run("GenericTypesAndInterfaces", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"models/repository.ts": `
export interface Repository<T> {
    find(id: string): T | undefined;
    save(item: T): void;
}

export class InMemoryRepository<T> implements Repository<T> {
    constructor(private items: Map<string, T>) {}
    
    find(id: string): T | undefined {
        return this.items.get(id);
    }
    
    save(item: T): void {
        // Implementation
    }
}`,
			"models/user-repo.ts": `
import { Repository, InMemoryRepository } from './repository';

interface User {
    id: string;
    name: string;
}

export class UserRepository extends InMemoryRepository<User> {
    constructor() {
        super(new Map());
    }
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"models/user-repo.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"models/repository.ts": true,
			"models/user-repo.ts": true,
		}, result)
	})

	// Test case 3: Decorators and metadata
	t.Run("DecoratorsAndMetadata", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"decorators/injectable.ts": `
export function Injectable() {
    return function (target: any) {
        // Decorator implementation
    };
}

export function Autowired() {
    return function (target: any, propertyKey: string) {
        // Decorator implementation
    };
}`,
			"services/data.ts": `
import { Injectable, Autowired } from '../decorators/injectable';

@Injectable()
export class DataService {
    @Autowired()
    private repository: any;
    
    getData(): void {
        // Implementation
    }
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"services/data.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"decorators/injectable.ts": true,
			"services/data.ts":        true,
		}, result)
	})

	// Test case 4: Utility types and type manipulation
	t.Run("UtilityTypesAndTypeManipulation", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"types/common.ts": `
export type DeepPartial<T> = {
    [P in keyof T]?: DeepPartial<T[P]>;
};

export type Nullable<T> = T | null;

export interface BaseEntity {
    id: string;
    createdAt: Date;
    updatedAt: Date;
}`,
			"models/product.ts": `
import { DeepPartial, Nullable, BaseEntity } from '../types/common';

export interface Product extends BaseEntity {
    name: string;
    price: number;
    description: Nullable<string>;
}

export type ProductUpdate = DeepPartial<Product>;`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"models/product.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"types/common.ts":   true,
			"models/product.ts": true,
		}, result)
	})

	// Test case 5: Namespaces and modules
	t.Run("NamespacesAndModules", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils/validation.ts": `
export namespace Validation {
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
			"services/validator.ts": `
import { Validation } from '../utils/validation';

export class EmailValidator extends Validation.RegexValidator {
    constructor() {
        super(/^[^@]+@[^@]+\.[^@]+$/);
    }
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"services/validator.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"utils/validation.ts":    true,
			"services/validator.ts":  true,
		}, result)
	})

	// Test case 6: Multiple imports from different files
	t.Run("MultipleImports", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"types/user.ts": `
export interface User {
    id: string;
    name: string;
    roles: Role[];
}`,
			"types/role.ts": `
export type Role = {
    id: string;
    permissions: Permission[];
};

export enum Permission {
    READ = 'read',
    WRITE = 'write',
    ADMIN = 'admin'
}`,
			"types/audit.ts": `
export interface AuditLog {
    timestamp: Date;
    action: string;
    userId: string;
}`,
			// This file shouldn't be included despite being in the types directory
			// as it's not imported by user-service.ts
			"types/notification.ts": `
export interface Notification {
    id: string;
    message: string;
    userId: string;
}

export type NotificationType = 'email' | 'sms' | 'push';`,
			"services/user-service.ts": `
import { User } from '../types/user';
import { Role, Permission } from '../types/role';
import { AuditLog } from '../types/audit';

export class UserService {
    createUser(user: User): void {
        // Implementation
    }

    assignRole(userId: string, role: Role): void {
        const log: AuditLog = {
            timestamp: new Date(),
            action: 'ROLE_ASSIGNED',
            userId
        };
        // Implementation
    }

    hasPermission(userId: string, permission: Permission): boolean {
        // Implementation
        return true;
    }
}`,
			// This file shouldn't be included despite using similar types
			// as it's not related to the entry point
			"services/notification-service.ts": `
import { Notification, NotificationType } from '../types/notification';
import { User } from '../types/user';

export class NotificationService {
    sendNotification(user: User, notification: Notification): void {
        // Implementation
    }
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"services/user-service.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"types/user.ts":           true,
			"types/role.ts":          true,
			"types/audit.ts":         true,
			"services/user-service.ts": true,
			// These files should NOT be included:
			// "types/notification.ts" - not imported by user-service.ts
			// "services/notification-service.ts" - not related to entry point
		}, result)
	})

	// Test case 7: Complex type composition across files
	t.Run("ComplexTypeComposition", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"models/base.ts": `
export interface Entity {
    id: string;
    createdAt: Date;
    updatedAt: Date;
}

export type Identifiable = Pick<Entity, 'id'>;
export type Timestamped = Pick<Entity, 'createdAt' | 'updatedAt'>;`,
			"models/validation.ts": `
export interface ValidationRule {
    validate(value: any): boolean;
    message: string;
}

export type ValidationResult = {
    isValid: boolean;
    errors: string[];
};`,
			"models/form.ts": `
import { ValidationRule, ValidationResult } from './validation';

export interface FormField<T> {
    value: T;
    rules: ValidationRule[];
    validate(): ValidationResult;
}`,
			// This file shouldn't be included despite extending Entity
			// as it's not imported by user-form.ts
			"models/product.ts": `
import { Entity } from './base';

export interface Product extends Entity {
    name: string;
    price: number;
}

export interface ProductValidation extends ValidationRule {
    validatePrice(price: number): boolean;
}`,
			"components/user-form.ts": `
import { Entity, Identifiable, Timestamped } from '../models/base';
import { FormField } from '../models/form';
import { ValidationRule } from '../models/validation';

interface UserFormData extends Entity {
    email: string;
    password: string;
}

class EmailRule implements ValidationRule {
    message = 'Invalid email';
    validate(value: string): boolean {
        return /^[^@]+@[^@]+\.[^@]+$/.test(value);
    }
}

export class UserForm {
    emailField: FormField<string> = {
        value: '',
        rules: [new EmailRule()],
        validate() {
            return { isValid: true, errors: [] };
        }
    };

    getData(): Identifiable & Timestamped {
        return {
            id: 'test',
            createdAt: new Date(),
            updatedAt: new Date()
        };
    }
}`,
			// This file shouldn't be included despite using similar validation logic
			// as it's not related to the entry point
			"components/product-form.ts": `
import { Product, ProductValidation } from '../models/product';
import { FormField } from '../models/form';

export class ProductForm {
    priceField: FormField<number> = {
        value: 0,
        rules: [],
        validate() {
            return { isValid: true, errors: [] };
        }
    };
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{"components/user-form.ts"}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"models/base.ts":          true,
			"models/validation.ts":    true,
			"models/form.ts":         true,
			"components/user-form.ts": true,
			// These files should NOT be included:
			// "models/product.ts" - not imported by user-form.ts
			// "components/product-form.ts" - not related to entry point
		}, result)
	})

	// Test case 8: Multiple entry points with shared dependencies
	t.Run("MultipleEntryPointsSharedDeps", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"config/logger.ts": `
export enum LogLevel {
    DEBUG,
    INFO,
    WARN,
    ERROR
}

export interface Logger {
    log(level: LogLevel, message: string): void;
}`,
			"config/database.ts": `
export interface DatabaseConfig {
    host: string;
    port: number;
    credentials: Credentials;
}

export interface Credentials {
    username: string;
    password: string;
}`,
			// This file shouldn't be included despite being in config
			// as it's not imported by any service
			"config/cache.ts": `
export interface CacheConfig {
    host: string;
    port: number;
    ttl: number;
}

export interface CacheOptions {
    compression: boolean;
    maxSize: number;
}`,
			"services/auth-service.ts": `
import { Logger, LogLevel } from '../config/logger';
import { DatabaseConfig } from '../config/database';

export class AuthService {
    constructor(
        private logger: Logger,
        private dbConfig: DatabaseConfig
    ) {}

    login(username: string, password: string): boolean {
        this.logger.log(LogLevel.INFO, 'Login attempt');
        return true;
    }
}`,
			"services/user-manager.ts": `
import { Logger, LogLevel } from '../config/logger';
import { DatabaseConfig, Credentials } from '../config/database';

export class UserManager {
    constructor(
        private logger: Logger,
        private dbConfig: DatabaseConfig
    ) {}

    createUser(creds: Credentials): void {
        this.logger.log(LogLevel.INFO, 'Creating user');
    }
}`,
			// This file shouldn't be included despite using logger
			// as it's not one of the entry points
			"services/cache-service.ts": `
import { Logger, LogLevel } from '../config/logger';
import { CacheConfig, CacheOptions } from '../config/cache';

export class CacheService {
    constructor(
        private logger: Logger,
        private config: CacheConfig
    ) {}

    configure(options: CacheOptions): void {
        this.logger.log(LogLevel.INFO, 'Configuring cache');
    }
}`,
		})
		ignoreRules := gitignore.CompileIgnoreLines(ignore.DefaultPatterns...)
		result, err := ResolveTypeAndFunctionFiles([]string{
			"services/auth-service.ts",
			"services/user-manager.ts",
		}, fsys, ignoreRules)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"config/logger.ts":         true,
			"config/database.ts":       true,
			"services/auth-service.ts": true,
			"services/user-manager.ts": true,
			// These files should NOT be included:
			// "config/cache.ts" - not imported by any entry point
			// "services/cache-service.ts" - not an entry point
		}, result)
	})
}