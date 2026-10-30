import { useState, useEffect } from 'react';
import { Lockbox } from './components/Lockbox';
import { Dashboard } from './components/Dashboard';
import { WailsBridge } from './services/wailsBridge';

export function App() {
  const [currentState, setCurrentState] = useState<'DISARMED' | 'ARMED' | 'TRIGGERED'>('DISARMED');
  const [isLocked, setIsLocked] = useState<boolean>(false);
  const [logs, setLogs] = useState<string[]>([]);

  useEffect(() => {
    // Fetch initial logs & status
    WailsBridge.getLogs().then((initialLogs) => {
      if (initialLogs && initialLogs.length > 0) {
        setLogs(initialLogs);
      }
    });

    WailsBridge.getStatus().then((status) => {
      setCurrentState(status.state as any);
      if (status.state === 'TRIGGERED') {
        setIsLocked(true);
      }
    });

    // Listen to real-time events from Go backend
    WailsBridge.onEvent('new-log', (newLog: string) => {
      setLogs((prev) => [...prev, newLog]);
    });

    WailsBridge.onEvent('status-change', (newState: string) => {
      setCurrentState(newState as any);
      if (newState === 'TRIGGERED') {
        setIsLocked(true);
      } else if (newState === 'DISARMED') {
        setIsLocked(false);
      }
    });

    WailsBridge.onEvent('breach-triggered', () => {
      setCurrentState('TRIGGERED');
      setIsLocked(true);
    });
  }, []);

  const handleTriggerTrap = async () => {
    await WailsBridge.triggerTrap();
    setCurrentState('TRIGGERED');
    setIsLocked(true);
  };

  const handleDisarmSuccess = () => {
    setCurrentState('DISARMED');
    setIsLocked(false);
  };

  return (
    <div className="w-screen h-screen overflow-hidden bg-[#050505] text-white relative">
      {isLocked ? (
        <Lockbox onDisarmSuccess={handleDisarmSuccess} logs={logs} />
      ) : (
        <Dashboard onTriggerTrap={handleTriggerTrap} logs={logs} />
      )}

      {/* FLOATING ARMED OVERLAY BADGE (RIGHT ABOVE TASKBAR) */}
      {currentState === 'ARMED' && !isLocked && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-4 px-6 py-2.5 bg-[#050505]/95 border-2 border-[#FF003C] shadow-[0_0_25px_rgba(255,0,60,0.6)] text-xs font-mono select-none">
          <div className="flex items-center gap-2.5">
            <span className="relative flex h-3 w-3">
              <span className="animate-ping absolute inline-flex h-full w-full bg-[#FF003C] opacity-75" />
              <span className="relative inline-flex h-3 w-3 bg-[#FF003C]" />
            </span>
            <span className="text-[#FF003C] font-black tracking-widest uppercase">
              SNOOPTRAP // ARMED &amp; GUARDIAN ACTIVE
            </span>
          </div>

          <div className="text-[11px] text-[#8F8F8F] border-l border-[#333] pl-3">
            Tekan <strong className="text-white">Ctrl + Alt + U</strong> atau tap tombol Telegram untuk Disarm
          </div>

          <button
            onClick={() => WailsBridge.disarmSystem('')}
            className="px-3 py-1 bg-[#FF003C] text-[#050505] font-black text-[10px] tracking-wider uppercase hover:bg-white transition-all cursor-pointer"
          >
            DISARM NOW
          </button>
        </div>
      )}
    </div>
  );
}

export default App;
