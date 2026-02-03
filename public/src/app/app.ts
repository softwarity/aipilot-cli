import { Component, CUSTOM_ELEMENTS_SCHEMA, LOCALE_ID, inject, PLATFORM_ID } from '@angular/core';
import { isPlatformBrowser, DOCUMENT } from '@angular/common';

const LANG_STORAGE_KEY = 'aipilot-lang';

@Component({
  selector: 'app-root',
  schemas: [CUSTOM_ELEMENTS_SCHEMA],
  templateUrl: './app.html',
  styleUrl: './app.scss'
})
export class App {
  readonly currentLocale = inject(LOCALE_ID);
  private readonly document = inject(DOCUMENT);
  private readonly platformId = inject(PLATFORM_ID);

  readonly languages = [
    { code: 'en', label: 'EN', path: 'en/' },
    { code: 'fr', label: 'FR', path: 'fr/' },
    { code: 'de', label: 'DE', path: 'de/' },
    { code: 'es', label: 'ES', path: 'es/' },
    { code: 'pt', label: 'PT', path: 'pt/' },
    { code: 'zh', label: 'ZH', path: 'zh/' },
    { code: 'ja', label: 'JA', path: 'ja/' },
    { code: 'hi', label: 'HI', path: 'hi/' },
    { code: 'vi', label: 'VI', path: 'vi/' },
    { code: 'ru', label: 'RU', path: 'ru/' },
  ];

  onLangClick(lang: { code: string; path: string }): void {
    if (!isPlatformBrowser(this.platformId)) return;

    // Sauvegarde le choix de langue AVANT la navigation
    const storage = this.document.defaultView?.localStorage;
    storage?.setItem(LANG_STORAGE_KEY, lang.code);
  }

  private getBaseHref(): string {
    if (isPlatformBrowser(this.platformId)) {
      const baseEl = this.document.querySelector('base');
      if (baseEl) {
        return baseEl.getAttribute('href') || '/';
      }
    }
    return '/';
  }

  getLangHref(lang: { code: string; path: string }): string {
    // Récupère le baseHref depuis le tag <base> (défini par Angular au build)
    // Ex: /aipilot-cli/fr/ -> on veut /aipilot-cli/{autre-lang}/
    let base = this.getBaseHref();

    // Enlève le suffixe locale courant du base pour obtenir la racine
    // /aipilot-cli/fr/ -> /aipilot-cli/
    // /en/ -> /
    base = base.replace(/[a-z]{2}\/$/, '');

    return base + lang.path;
  }
}
