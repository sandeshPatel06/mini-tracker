export namespace db {
	
	export class LogEntry {
	    id: number;
	    // Go type: time
	    timestamp: any;
	    image_path: string;
	    total_keys: number;
	    unique_keys: number;
	    entropy_score: number;
	    ai_category: string;
	    is_productive: boolean;
	    ai_confidence: number;
	    ai_reason: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.image_path = source["image_path"];
	        this.total_keys = source["total_keys"];
	        this.unique_keys = source["unique_keys"];
	        this.entropy_score = source["entropy_score"];
	        this.ai_category = source["ai_category"];
	        this.is_productive = source["is_productive"];
	        this.ai_confidence = source["ai_confidence"];
	        this.ai_reason = source["ai_reason"];
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

