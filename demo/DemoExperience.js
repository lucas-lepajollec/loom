// Pass 9 Ecosystem Standard Demo Chrome for Loom (EN default + FR localized)
(function() {
  const INTRO_KEY = 'lh-demo-intro-seen';
  const LINKS = {
    site: 'https://loom.lucas-homelab.fr',
    docs: 'https://docs.loom.lucas-homelab.fr',
    source: 'https://github.com/lucas-lepajollec/loom',
  };

  const COPY = {
    en: {
      badge: 'ISOLATED PUBLIC DEMO',
      title: 'Explore the Loom workstation interface, with zero local daemon.',
      body: 'This is a browser-only interactive preview of <strong>Loom</strong>. Discussions, local and cloud models, Codeman agents, Second Brain notes, machines, and quotas are simulated locally in your browser with fictional data.',
      cards: [
        {
          title: 'You can try',
          text: 'Send messages with simulated streaming replies, switch local and cloud models, inspect Codeman agent sessions and terminals, edit Second Brain Markdown notes, and browse machines, quotas, and policies.',
        },
        {
          title: 'What is simulated',
          text: 'GPU VRAM usage, llama.cpp slots, token streaming, Codeman sessions, machine telemetry, and subscription quotas are synthetic fixtures designed to demonstrate the real UI.',
        },
        {
          title: 'What never happens',
          text: 'No GGUF weights are downloaded, no real cloud API or local engine is called, no personal keys are requested, and nothing is installed on your machine.',
        },
      ],
      limits: 'Edits you make stay inside this browser session. Use Reset at any time to restore the default fictional sandbox state.',
      navLabel: 'Ecosystem links',
      linkSite: 'Website',
      linkDocs: 'Documentation',
      linkSource: 'Source on GitHub',
      reset: 'Reset',
      continueBtn: 'Continue to demo',
      chipLabel: 'DEMO',
      chipInfoAria: 'Demo information',
      chipResetTitle: 'Reset demo state',
      chipResetAria: 'Reset demo state',
    },
    fr: {
      badge: 'DÉMONSTRATION PUBLIQUE ISOLÉE',
      title: "Testez l'interface Loom, sans aucun démon local.",
      body: "Ceci est un aperçu interactif de <strong>Loom</strong> exécuté dans votre navigateur. Les discussions, modèles locaux et cloud, sessions d'agents Codeman, notes du second cerveau, machines et quotas sont simulés à partir de données fictives.",
      cards: [
        {
          title: 'Vous pouvez tester',
          text: "Envoyez des prompts avec réponses en streaming simulé, changez de modèle local ou cloud, inspectez les agents Codeman et leurs terminaux, éditez les notes Markdown et parcourez machines, quotas et autorisations.",
        },
        {
          title: 'Ce qui est simulé',
          text: "La VRAM GPU, les slots llama.cpp, le streaming de tokens, les sessions Codeman, la télémétrie machine et les quotas d'abonnement sont des données synthétiques illustrant l'interface réelle.",
        },
        {
          title: "Ce qui n'arrive jamais",
          text: "Aucun fichier GGUF n'est téléchargé, aucun moteur local ni API cloud réelle n'est contacté, aucune clé personnelle n'est demandée et rien n'est installé sur votre poste.",
        },
      ],
      limits: 'Vos modifications restent dans cette session navigateur. Cliquez sur Réinitialiser à tout moment pour restaurer les données fictives.',
      navLabel: 'Liens écosystème',
      linkSite: 'Site officiel',
      linkDocs: 'Documentation',
      linkSource: 'Code source sur GitHub',
      reset: 'Réinitialiser',
      continueBtn: 'Accéder à la démo',
      chipLabel: 'DÉMO',
      chipInfoAria: 'Informations sur la démo',
      chipResetTitle: 'Réinitialiser la démo',
      chipResetAria: 'Réinitialiser la démonstration',
    },
  };

  function resolveLang() {
    try {
      const search = typeof window !== 'undefined' && window.location && window.location.search;
      if (search) {
        const q = new URLSearchParams(search).get('lang');
        if (q === 'fr' || q === 'en') return q;
      }
    } catch {}
    try {
      const htmlLang = document.documentElement && document.documentElement.lang;
      if (htmlLang === 'fr' || htmlLang === 'en') return htmlLang;
    } catch {}
    try {
      const saved = localStorage.getItem('loom-lang');
      if (saved === 'fr' || saved === 'en') return saved;
    } catch {}
    return 'en';
  }

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
        if (key === INTRO_KEY || key.startsWith('loom_demo_') || key.startsWith('loom.')) {
          sessionStorage.removeItem(key);
        }
      }
      for (const key of Object.keys(localStorage)) {
        if (key.startsWith('loom-') || key.startsWith('loom.')) {
          localStorage.removeItem(key);
        }
      }
    } catch {}
    window.location.reload();
  }

  function dialogMarkup(lang) {
    const c = COPY[lang] || COPY.en;
    return `
      <div class="lh-demo-dialog-content">
        <div class="lh-demo-badge">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
          <span>${c.badge}</span>
        </div>

        <h2 class="lh-demo-title" id="lh-demo-title" tabindex="-1">${c.title}</h2>
        <p class="lh-demo-body">${c.body}</p>

        <div class="lh-demo-cards">
          ${c.cards.map(card => `
            <div class="lh-demo-card">
              <div class="lh-demo-card-title">${card.title}</div>
              <div class="lh-demo-card-text">${card.text}</div>
            </div>
          `).join('')}
        </div>

        <div class="lh-demo-limits">${c.limits}</div>

        <nav class="lh-demo-nav" aria-label="${c.navLabel}">
          <a href="${LINKS.site}" target="_blank" rel="noopener noreferrer">${c.linkSite}</a>
          <a href="${LINKS.docs}" target="_blank" rel="noopener noreferrer">${c.linkDocs}</a>
          <a href="${LINKS.source}" target="_blank" rel="noopener noreferrer">${c.linkSource}</a>
        </nav>

        <div class="lh-demo-actions">
          <button type="button" class="lh-demo-btn-reset" id="lh-demo-modal-reset">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
            <span>${c.reset}</span>
          </button>
          <button type="button" class="lh-demo-btn-continue" id="lh-demo-modal-continue">
            <span>${c.continueBtn}</span>
          </button>
        </div>
      </div>
    `;
  }

  function chipMarkup(lang) {
    const c = COPY[lang] || COPY.en;
    return `
      <div class="lh-demo-chip-inner">
        <button type="button" class="lh-demo-chip-btn" id="lh-demo-chip-info" aria-label="${c.chipInfoAria}">
          <span>${c.chipLabel}</span>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="opacity: 0.6" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg>
        </button>
        <div class="lh-demo-chip-divider"></div>
        <button type="button" class="lh-demo-chip-reset" id="lh-demo-chip-reset" title="${c.chipResetTitle}" aria-label="${c.chipResetAria}">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
        </button>
      </div>
    `;
  }

  function initDemoExperience() {
    let activeLang = resolveLang();

    const dialog = document.createElement('dialog');
    dialog.className = 'lh-demo-dialog';
    dialog.id = 'lh-demo-modal';
    dialog.setAttribute('aria-labelledby', 'lh-demo-title');

    const chip = document.createElement('div');
    chip.className = 'lh-demo-chip';
    chip.id = 'lh-demo-chip';

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
      const heading = dialog.querySelector('#lh-demo-title');
      if (heading && typeof heading.focus === 'function') {
        heading.focus({ preventScroll: true });
      }
      dialog.scrollTop = 0;
    };

    const render = (lang) => {
      activeLang = lang === 'fr' ? 'fr' : 'en';
      dialog.innerHTML = dialogMarkup(activeLang);
      chip.innerHTML = chipMarkup(activeLang);
      const continueBtn = document.getElementById('lh-demo-modal-continue');
      const modalReset = document.getElementById('lh-demo-modal-reset');
      const chipInfo = document.getElementById('lh-demo-chip-info');
      const chipReset = document.getElementById('lh-demo-chip-reset');
      if (continueBtn) continueBtn.addEventListener('click', closeModal);
      if (modalReset) modalReset.addEventListener('click', resetDemo);
      if (chipInfo) chipInfo.addEventListener('click', openModal);
      if (chipReset) chipReset.addEventListener('click', resetDemo);
    };

    render(activeLang);

    dialog.addEventListener('cancel', (e) => {
      e.preventDefault();
      closeModal();
    });

    dialog.addEventListener('click', (e) => {
      if (e.target === dialog) closeModal();
    });

    // Keep demo chrome synchronized when the user changes language in Settings.
    if (typeof MutationObserver === 'function' && document.documentElement) {
      const obs = new MutationObserver(() => {
        const next = document.documentElement.lang === 'fr' ? 'fr' : 'en';
        if (next !== activeLang) render(next);
      });
      obs.observe(document.documentElement, { attributes: true, attributeFilter: ['lang'] });
    }

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
