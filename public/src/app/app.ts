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
    { code: 'en', label: 'EN' },
    { code: 'fr', label: 'FR' },
    { code: 'de', label: 'DE' },
    { code: 'es', label: 'ES' },
    { code: 'pt', label: 'PT' },
    { code: 'zh', label: 'ZH' },
    { code: 'ja', label: 'JA' },
    { code: 'hi', label: 'HI' },
    { code: 'vi', label: 'VI' },
    { code: 'ru', label: 'RU' },
  ];

  onLangClick(lang: { code: string }): void {
    if (!isPlatformBrowser(this.platformId)) return;
    const storage = this.document.defaultView?.localStorage;
    storage?.setItem(LANG_STORAGE_KEY, lang.code);
  }

  getLangHref(lang: { code: string }): string {
    // Chemin relatif: depuis /aipilot-cli/fr/ vers /aipilot-cli/en/
    return `../${lang.code}/`;
  }
}
