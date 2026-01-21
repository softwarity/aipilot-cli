# Privacy Policy for AIPilot

**Last updated: January 21, 2026**

## Introduction

AIPilot ("we", "our", or "us") is committed to protecting your privacy. This Privacy Policy explains how the AIPilot mobile application and AIPilot CLI collect, use, and safeguard your information.

## Information We Collect

### Information You Provide
- **SSH Connection Details**: Host addresses, ports, and usernames for SSH connections (Pro version only)
- **Agent Configurations**: Names and working directories for your AI agents

### Information Collected Automatically
- **Voice Input**: Audio captured when you use the voice recognition feature is processed locally on your device and converted to text. We do not store or transmit audio recordings.
- **Camera Access**: Used solely to scan QR codes for CLI connection. Images are processed locally and not stored or transmitted.

### Information We Do NOT Collect
- We do not collect personal identification information
- We do not track your location
- We do not access your contacts, photos, or files (except when you explicitly choose to upload files in Pro version)
- We do not use analytics or tracking services
- We do not store your terminal session content

## How We Use Your Information

- **SSH Credentials**: Stored locally on your device using secure storage. Used only to establish connections to your servers.
- **Voice Data**: Converted to text commands on your device using your device's speech recognition. Text is sent to your connected AI agent.
- **QR Code Scanning**: Used only to extract connection information for CLI mode.

## Data Storage and Security

### Local Storage
- All sensitive data (SSH private keys, connection configurations) is stored locally on your device using Flutter Secure Storage (encrypted storage).
- Session history is stored locally for your convenience.

### Data Transmission
- **CLI Mode**: Communication between your mobile device and PC goes through our relay server. The relay only forwards encrypted data and does not store any session content.
- **SSH Mode**: Direct encrypted SSH connection between your device and your server. No data passes through our servers.

### What We Store on Our Servers
- **Nothing**. We do not store any user data, session content, credentials, or personal information on our servers.

## Third-Party Services

AIPilot uses the following third-party services:
- **Device Speech Recognition**: Your device's built-in speech-to-text service (Google Speech Services on Android). Please refer to Google's privacy policy for information about how voice data is processed.

## Your Rights

You have the right to:
- **Delete your data**: All data is stored locally. Uninstalling the app removes all stored data.
- **Access your data**: You can view all stored configurations within the app settings.
- **Control permissions**: You can revoke microphone or camera permissions at any time through your device settings.

## Children's Privacy

AIPilot is not intended for use by children under 13 years of age. We do not knowingly collect information from children under 13.

## Changes to This Policy

We may update this Privacy Policy from time to time. We will notify you of any changes by updating the "Last updated" date at the top of this policy.

## Open Source

AIPilot CLI is open source. You can review the code at:
- CLI: https://github.com/softwarity/aipilot-cli

## Contact Us

If you have questions about this Privacy Policy, please contact us at:
- Email: contact@softwarity.io
- GitHub: https://github.com/softwarity/aipilot-cli/issues

## Summary

| Data Type | Collected | Stored Locally | Sent to Our Servers |
|-----------|-----------|----------------|---------------------|
| Voice/Audio | Yes (temporary) | No | No |
| Camera/QR | Yes (temporary) | No | No |
| SSH Credentials | Yes | Yes (encrypted) | No |
| Session Content | No | No | No |
| Personal Info | No | No | No |
| Analytics | No | No | No |

---

© 2026 Softwarity. All rights reserved.
