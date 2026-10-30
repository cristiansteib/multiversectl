// The polite live region at the end of the body, for the one-off messages a
// press produces ("Copied to clipboard").
export function say(msg: string): void {
  const status = document.getElementById('status');
  if (!status) return;
  status.textContent = msg;
  setTimeout(() => {
    status.textContent = '';
  }, 2000);
}
