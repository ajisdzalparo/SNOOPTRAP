export namespace audio {
	
	export class AudioDevice {
	    id: string;
	    name: string;
	    isDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AudioDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.isDefault = source["isDefault"];
	    }
	}

}

export namespace config {
	
	export class Config {
	    telegramBotToken: string;
	    telegramChatId: string;
	    idleThresholdSeconds: number;
	    targetAudioDeviceGuid: string;
	    autoArm: boolean;
	    nuclearAction: string;
	    targetKillProcess: string;
	    videoRecordDurationSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.telegramBotToken = source["telegramBotToken"];
	        this.telegramChatId = source["telegramChatId"];
	        this.idleThresholdSeconds = source["idleThresholdSeconds"];
	        this.targetAudioDeviceGuid = source["targetAudioDeviceGuid"];
	        this.autoArm = source["autoArm"];
	        this.nuclearAction = source["nuclearAction"];
	        this.targetKillProcess = source["targetKillProcess"];
	        this.videoRecordDurationSeconds = source["videoRecordDurationSeconds"];
	    }
	}

}

export namespace main {
	
	export class SystemStatus {
	    state: string;
	    idleSeconds: number;
	    isArmed: boolean;
	    hasOtp: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SystemStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.idleSeconds = source["idleSeconds"];
	        this.isArmed = source["isArmed"];
	        this.hasOtp = source["hasOtp"];
	    }
	}

}

