import { VoiceConnectionStatus, entersState, type VoiceConnection } from "@discordjs/voice";

// A disconnected connection remains registered with Discord's voice library. Reusing
// that object does not send another join request; explicitly rejoin and await Ready.
export async function ensureConnectionReady(connection: VoiceConnection, channelId: string, timeoutMs: number): Promise<void> {
  if (connection.state.status === VoiceConnectionStatus.Disconnected) {
    if (!connection.rejoin({ channelId })) throw new Error("The Discord voice connection could not rejoin.");
  }
  if (connection.state.status === VoiceConnectionStatus.Destroyed) {
    throw new Error("The Discord voice connection was closed.");
  }
  try {
    await entersState(connection, VoiceConnectionStatus.Ready, timeoutMs);
  } catch (cause) {
    throw new Error("The Discord voice connection did not become ready.", { cause });
  }
}


// Brief transport interruptions can recover on their own. A session still not
// Ready after the grace period is lost, even if its object was not destroyed.
export function watchConnectionLoss(connection: VoiceConnection, onLost: () => void, graceMs = 5000): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const clear = () => { clearTimeout(timer); timer = undefined; };
  const dispose = () => { clear(); connection.off("stateChange", changed); };
  const changed = () => {
    const status = connection.state.status;
    if (status === VoiceConnectionStatus.Destroyed) { dispose(); return; }
    if (status === VoiceConnectionStatus.Ready) { clear(); return; }
    if (status !== VoiceConnectionStatus.Disconnected || timer !== undefined) return;
    timer = setTimeout(() => {
      dispose();
      if (connection.state.status !== VoiceConnectionStatus.Ready
        && connection.state.status !== VoiceConnectionStatus.Destroyed) onLost();
    }, graceMs);
  };
  connection.on("stateChange", changed);
  changed();
  return dispose;
}
