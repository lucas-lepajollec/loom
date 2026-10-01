let pwaInstallPrompt=null;
function pwaRefresh(){
  const installed=document.documentElement.dataset.pwa==='1',button=wsEl('pwa-install'),status=wsEl('pwa-install-status');
  if(!button||!status)return;
  button.hidden=installed||!pwaInstallPrompt;
  status.textContent=installed?'Loom est ouvert comme application.':!window.isSecureContext?'L’installation et les notifications nécessitent HTTPS (sauf localhost). Sur le LAN HTTP, utilisez Loom dans le navigateur.':/iphone|ipad|ipod/i.test(navigator.userAgent)?'Dans Safari : Partager → Ajouter à l’écran d’accueil.':pwaInstallPrompt?'Ouvrez Loom dans sa propre fenêtre.':'Si disponible, utilisez « Installer l’application » dans le menu de votre navigateur.';
  const network=wsEl('app-network-status');if(network)network.hidden=navigator.onLine!==false;
}
window.addEventListener('beforeinstallprompt',event=>{event.preventDefault();pwaInstallPrompt=event;pwaRefresh();});
window.addEventListener('appinstalled',()=>{pwaInstallPrompt=null;pwaRefresh();wsEl('pwa-install-status').textContent='Loom est installé. Ouvrez-le depuis son icône.';});
window.addEventListener('online',pwaRefresh);window.addEventListener('offline',pwaRefresh);
document.addEventListener('DOMContentLoaded',()=>{
  pwaRefresh();
  wsEl('pwa-install').addEventListener('click',async()=>{
    const prompt=pwaInstallPrompt;if(!prompt)return;
    pwaInstallPrompt=null;pwaRefresh();await prompt.prompt();await prompt.userChoice;
  });
});
