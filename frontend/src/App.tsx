import { useState, useEffect } from 'react';
import { Lockbox } from './components/Lockbox';
import { Dashboard } from './components/Dashboard';
import { WailsBridge } from './services/wailsBridge';

export function App() {
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
      if (status.state === 'TRIGGERED') {
        setIsLocked(true);
      }
    });

    // Listen to real-time events from Go backend
    WailsBridge.onEvent('new-log', (newLog: string) => {
      setLogs((prev) => [...prev, newLog]);
    });

    WailsBridge.onEvent('status-change', (newState: string) => {
      if (newState === 'TRIGGERED') {
        setIsLocked(true);
      } else if (newState === 'DISARMED') {
        setIsLocked(false);
      }
    });

    WailsBridge.onEvent('breach-triggered', () => {
      setIsLocked(true);
    });
  }, []);

  const handleTriggerTrap = async () => {
    await WailsBridge.triggerTrap();
    setIsLocked(true);
  };

  const handleDisarmSuccess = () => {
    setIsLocked(false);
  };

  return (
    <div className="w-screen h-screen overflow-hidden bg-[#050505] text-white">
      {isLocked ? (
        <Lockbox onDisarmSuccess={handleDisarmSuccess} logs={logs} />
      ) : (
        <Dashboard onTriggerTrap={handleTriggerTrap} logs={logs} />
      )}
    </div>
  );
}

export default App;
