// Pass 9 Ecosystem Standard Demo Chrome for Loom
(function() {
  const INTRO_KEY = 'lh-demo-intro-seen';
  const LINKS = {
    site: 'https://loom.lucas-homelab.fr',
    docs: 'https://docs.loom.lucas-homelab.fr',
  };

  function hasSeenIntro() {
    try {
      return sessionStorage.getItem(INTRO_KEY) === '1';
    } catch {
      return false;
    }
  }

  function markIntroSeen() {
    try {
      sessionStorage.setItem(INTRO_KEY, '1');
    } catch {}
  }

  function resetDemo() {
    try {
      // Reset only Loom's demo state. A full origin-wide clear could erase
      // unrelated preferences if this demo is ever hosted under a shared origin.
      for (const key of Object.keys(sessionStorage)) {
        if (key === INTRO_KEY || key.startsWith('loom_demo_')) sessionStorage.removeItem(key);
      }
      for (const key of Object.keys(localStorage)) {
        if (key.startsWith('loom-') || key.startsWith('loom.')) localStorage.removeItem(key);
      }
    } catch {}
    window.location.reload();
  }

  function initDemoExperience() {
    // Inject Dialog
    const dialog = document.createElement('dialog');
    dialog.className = 'lh-demo-dialog';
    dialog.id = 'lh-demo-modal';
    dialog.setAttribute('aria-labelledby', 'lh-demo-title');
    dialog.innerHTML = `
      <div class="lh-demo-dialog-content">
        <div class="lh-demo-badge">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
          <span>DÉMONSTRATION PUBLIQUE ISOLÉE</span>
        </div>

        <h2 class="lh-demo-title" id="lh-demo-title" tabindex="-1">Testez l'interface station de travail Loom, sans aucun démon local.</h2>
        <p class="lh-demo-body">
          Ceci est un aperçu interactif de <strong>Loom</strong>. La recherche de modèles, le téléchargement, l'inférence et les benchmarks sont simulés dans votre navigateur. Aucun vrai modèle n'est téléchargé, aucun moteur local n'est lancé et aucun GPU physique n'est requis.
        </p>

        <div class="lh-demo-cards">
          <div class="lh-demo-card">
            <div class="lh-demo-card-title">Vous pouvez tester</div>
            <div class="lh-demo-card-text">Parcourez le catalogue de démonstration, simulez le téléchargement et le chargement de modèles, ajustez les paramètres d'inférence (contexte, couches GPU, température) et testez le chat avec réflexion.</div>
          </div>
          <div class="lh-demo-card">
            <div class="lh-demo-card-title">Ce qui est simulé</div>
            <div class="lh-demo-card-text">L'allocation VRAM, la consommation RAM, le débit tok/s et le téléchargement GGUF sont des valeurs fictives destinées à montrer l'interface, pas des mesures réelles.</div>
          </div>
          <div class="lh-demo-card">
            <div class="lh-demo-card-title">Ce qui n'arrive jamais</div>
            <div class="lh-demo-card-text">Aucun fichier GGUF lourd n'est écrit sur votre disque, aucune clé privée n'est demandée et aucun démon d'arrière-plan n'est installé.</div>
          </div>
        </div>

        <div class="lh-demo-limits">
          Certains choix peuvent rester enregistrés dans ce navigateur. Cliquez sur Réinitialiser pour restaurer les données de démonstration.
        </div>

        <nav class="lh-demo-nav" aria-label="Liens écosystème">
          <a href="${LINKS.site}" target="_blank" rel="noreferrer">Site officiel</a>
          <a href="${LINKS.docs}" target="_blank" rel="noreferrer">Documentation technique</a>
        </nav>

        <div class="lh-demo-actions">
          <button type="button" class="lh-demo-btn-reset" id="lh-demo-modal-reset">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
            <span>Réinitialiser</span>
          </button>
          <button type="button" class="lh-demo-btn-continue" id="lh-demo-modal-continue">
            <span>Accéder à la démo</span>
          </button>
        </div>
      </div>
    `;

    // Inject Chip
    const chip = document.createElement('div');
    chip.className = 'lh-demo-chip';
    chip.id = 'lh-demo-chip';
    chip.innerHTML = `
      <div class="lh-demo-chip-inner">
        <button type="button" class="lh-demo-chip-btn" id="lh-demo-chip-info" aria-label="Informations sur la démo">
          <span>DÉMO</span>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="opacity: 0.6"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg>
        </button>
        <div class="lh-demo-chip-divider"></div>
        <button type="button" class="lh-demo-chip-reset" id="lh-demo-chip-reset" title="Réinitialiser la démo" aria-label="Réinitialiser la démonstration">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
        </button>
      </div>
    `;

    document.body.appendChild(dialog);
    document.body.appendChild(chip);

    const closeModal = () => {
      markIntroSeen();
      dialog.close();
      chip.classList.remove('invisible');
    };

    const openModal = () => {
      chip.classList.add('invisible');
      dialog.showModal();
      // On a narrow screen the actions sit below the scrollable introduction.
      // Focus the heading instead of jumping straight to the bottom button.
      dialog.querySelector('#lh-demo-title').focus({ preventScroll: true });
      dialog.scrollTop = 0;
    };

    document.getElementById('lh-demo-modal-continue').addEventListener('click', closeModal);
    document.getElementById('lh-demo-modal-reset').addEventListener('click', resetDemo);
    document.getElementById('lh-demo-chip-info').addEventListener('click', openModal);
    document.getElementById('lh-demo-chip-reset').addEventListener('click', resetDemo);

    dialog.addEventListener('cancel', (e) => {
      e.preventDefault();
      closeModal();
    });

    dialog.addEventListener('click', (e) => {
      if (e.target === dialog) closeModal();
    });

    if (!hasSeenIntro()) {
      openModal();
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initDemoExperience);
  } else {
    initDemoExperience();
  }
})();
