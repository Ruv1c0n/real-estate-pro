FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1
WORKDIR /srv
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY fake_core_api.py .
RUN useradd -m appuser
USER appuser
EXPOSE 8000
CMD ["uvicorn", "fake_core_api:app", "--host", "0.0.0.0", "--port", "8000"]
