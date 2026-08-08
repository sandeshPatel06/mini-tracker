export namespace db {
	
	export class LogEntry {
	    id: number;
	    org_id: number;
	    user_id: number;
	    // Go type: time
	    timestamp: any;
	    image_path: string;
	    total_keys: number;
	    unique_keys: number;
	    entropy_score: number;
	    app_name: string;
	    app_category: string;
	    window_title: string;
	    session_id: number;
	    session_title: string;
	    ai_category: string;
	    is_productive: boolean;
	    productive_score: number;
	    ai_confidence: number;
	    ai_reason: string;
	    sync_status: string;
	    remote_id: number;
	    // Go type: time
	    synced_at: any;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.org_id = source["org_id"];
	        this.user_id = source["user_id"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.image_path = source["image_path"];
	        this.total_keys = source["total_keys"];
	        this.unique_keys = source["unique_keys"];
	        this.entropy_score = source["entropy_score"];
	        this.app_name = source["app_name"];
	        this.app_category = source["app_category"];
	        this.window_title = source["window_title"];
	        this.session_id = source["session_id"];
	        this.session_title = source["session_title"];
	        this.ai_category = source["ai_category"];
	        this.is_productive = source["is_productive"];
	        this.productive_score = source["productive_score"];
	        this.ai_confidence = source["ai_confidence"];
	        this.ai_reason = source["ai_reason"];
	        this.sync_status = source["sync_status"];
	        this.remote_id = source["remote_id"];
	        this.synced_at = this.convertValues(source["synced_at"], null);
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
	export class ProductivityStats {
	    date: string;
	    total_minutes: number;
	    productive_minutes: number;
	    unproductive_minutes: number;
	    avg_entropy_score: number;
	    top_category: string;
	
	    static createFrom(source: any = {}) {
	        return new ProductivityStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.total_minutes = source["total_minutes"];
	        this.productive_minutes = source["productive_minutes"];
	        this.unproductive_minutes = source["unproductive_minutes"];
	        this.avg_entropy_score = source["avg_entropy_score"];
	        this.top_category = source["top_category"];
	    }
	}

}

