export namespace events {
	
	export class Event {
	    Type: string;
	    Data: Record<string, any>;
	    // Go type: time
	    Timestamp: any;
	    Source: string;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Type = source["Type"];
	        this.Data = source["Data"];
	        this.Timestamp = this.convertValues(source["Timestamp"], null);
	        this.Source = source["Source"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class ServerCapabilitiesInfo {
	    prefix: Record<string, string>;
	    prefix_string: string;
	    chanmodes: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerCapabilitiesInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prefix = source["prefix"];
	        this.prefix_string = source["prefix_string"];
	        this.chanmodes = source["chanmodes"];
	    }
	}
	export class ChannelInfo {
	    channel?: storage.Channel;
	    users: storage.ChannelUser[];
	    capabilities?: ServerCapabilitiesInfo;
	
	    static createFrom(source: any = {}) {
	        return new ChannelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel = this.convertValues(source["channel"], storage.Channel);
	        this.users = this.convertValues(source["users"], storage.ChannelUser);
	        this.capabilities = this.convertValues(source["capabilities"], ServerCapabilitiesInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LastOpenPane {
	    network_id: number;
	    type: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new LastOpenPane(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.network_id = source["network_id"];
	        this.type = source["type"];
	        this.name = source["name"];
	    }
	}
	export class ServerConfig {
	    address: string;
	    port: number;
	    tls: boolean;
	    order: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.port = source["port"];
	        this.tls = source["tls"];
	        this.order = source["order"];
	    }
	}
	export class NetworkConfig {
	    name: string;
	    address: string;
	    port: number;
	    tls: boolean;
	    servers: ServerConfig[];
	    nickname: string;
	    username: string;
	    realname: string;
	    password: string;
	    sasl_enabled: boolean;
	    sasl_mechanism: string;
	    sasl_username: string;
	    sasl_password: string;
	    sasl_external_cert: string;
	    auto_connect: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NetworkConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.address = source["address"];
	        this.port = source["port"];
	        this.tls = source["tls"];
	        this.servers = this.convertValues(source["servers"], ServerConfig);
	        this.nickname = source["nickname"];
	        this.username = source["username"];
	        this.realname = source["realname"];
	        this.password = source["password"];
	        this.sasl_enabled = source["sasl_enabled"];
	        this.sasl_mechanism = source["sasl_mechanism"];
	        this.sasl_username = source["sasl_username"];
	        this.sasl_password = source["sasl_password"];
	        this.sasl_external_cert = source["sasl_external_cert"];
	        this.auto_connect = source["auto_connect"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PluginInfo {
	    name: string;
	    version: string;
	    description?: string;
	    author?: string;
	    events?: string[];
	    permissions?: string[];
	    metadata_types?: string[];
	    config_schema?: Record<string, any>;
	    path: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.author = source["author"];
	        this.events = source["events"];
	        this.permissions = source["permissions"];
	        this.metadata_types = source["metadata_types"];
	        this.config_schema = source["config_schema"];
	        this.path = source["path"];
	        this.enabled = source["enabled"];
	    }
	}
	

}

export namespace storage {
	
	export class Channel {
	    id: number;
	    network_id: number;
	    name: string;
	    topic: string;
	    modes: string;
	    auto_join: boolean;
	    is_open: boolean;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new Channel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.network_id = source["network_id"];
	        this.name = source["name"];
	        this.topic = source["topic"];
	        this.modes = source["modes"];
	        this.auto_join = source["auto_join"];
	        this.is_open = source["is_open"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChannelUser {
	    id: number;
	    channel_id: number;
	    nickname: string;
	    modes: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new ChannelUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channel_id = source["channel_id"];
	        this.nickname = source["nickname"];
	        this.modes = source["modes"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Message {
	    id: number;
	    network_id: number;
	    channel_id?: number;
	    user: string;
	    message: string;
	    message_type: string;
	    // Go type: time
	    timestamp: any;
	    raw_line: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.network_id = source["network_id"];
	        this.channel_id = source["channel_id"];
	        this.user = source["user"];
	        this.message = source["message"];
	        this.message_type = source["message_type"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.raw_line = source["raw_line"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Network {
	    id: number;
	    name: string;
	    address: string;
	    port: number;
	    tls: boolean;
	    nickname: string;
	    username: string;
	    realname: string;
	    password: string;
	    sasl_enabled: boolean;
	    sasl_mechanism?: string;
	    sasl_username?: string;
	    sasl_password?: string;
	    sasl_external_cert?: string;
	    auto_connect: boolean;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Network(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.address = source["address"];
	        this.port = source["port"];
	        this.tls = source["tls"];
	        this.nickname = source["nickname"];
	        this.username = source["username"];
	        this.realname = source["realname"];
	        this.password = source["password"];
	        this.sasl_enabled = source["sasl_enabled"];
	        this.sasl_mechanism = source["sasl_mechanism"];
	        this.sasl_username = source["sasl_username"];
	        this.sasl_password = source["sasl_password"];
	        this.sasl_external_cert = source["sasl_external_cert"];
	        this.auto_connect = source["auto_connect"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Server {
	    id: number;
	    network_id: number;
	    address: string;
	    port: number;
	    tls: boolean;
	    order: number;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.network_id = source["network_id"];
	        this.address = source["address"];
	        this.port = source["port"];
	        this.tls = source["tls"];
	        this.order = source["order"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

